package main

import (
    "log"
    "github.com/go-fsnotify/fsnotify"
    "os"
    "io"
    "net/http"
    "io/ioutil"
    "mime/multipart"
    "bytes"
    "time"
    "strings"
    "gopkg.in/yaml.v2"
    "bufio"
    "fmt"
    "path/filepath"
    "path"
)

type Config struct {
    Paths, Exts, Urls []string
    Retry             uint64
}

type SendInfo struct {
    file, url string
}

var (
    config Config
    log2   *log.Logger
    lh     *os.File
    sent   = make(map[string]int)
)

func main() {
    defer lh.Close()
    done := make(chan bool)
    queue := make(chan SendInfo)
    go watch(queue)
    go upload(queue)
    <-done
}

func init() {
    // 初始化日志文件
    log.SetFlags(log.Ldate | log.Lmicroseconds)
    dir, err := filepath.Abs(filepath.Dir(os.Args[0]))
    if err != nil {
        log.Println("工作目录读取失败", err)
    }
    if strings.HasSuffix(dir, "\\") {
        dir += "log"
    } else {
        dir += "\\log"
    }
    if _, err := os.Stat(dir); err != nil || os.IsNotExist(err) {
        if err = os.MkdirAll(dir, 777); err != nil {
            log.Println("日志文件夹", dir, "创建失败", err)
        }
    }
    lf := dir + "\\" + time.Now().Format("20060102-150405") + ".log"
    if lh, err = os.Create(lf); err != nil {
        log.Println("日志文件", lf, "创建失败", err)
    }
    log2 = log.New(lh, "", log.Ldate|log.Lmicroseconds)
    logging("日志文件 " + lf + " 创建成功")

    // 加载配置文件
    configBytes, err := ioutil.ReadFile("config.yaml")
    if err != nil {
        fatal("配置文件加载失败: " + fmt.Sprint(err))
    }
    if err = yaml.Unmarshal(configBytes, &config); err != nil {
        fatal("配置文件读取失败: " + fmt.Sprint(err))
    }
    for _, p := range config.Paths {
        if _, err = os.Stat(p); err != nil || os.IsNotExist(err) {
            if err = os.MkdirAll(p, 777); err != nil {
                fatal("文件夹 " + p + " 创建失败，请检查配置文件: " + fmt.Sprint(err))
            }
            logging("文件夹 " + p + " 创建成功")
        }
    }
    for k, v := range config.Exts {
        if !strings.HasPrefix(v, ".") {
            config.Exts[k] = "." + v
        }
    }
}

func watch(queue chan<- SendInfo) {
    watcher, err := fsnotify.NewWatcher()
    if err != nil {
        fatal("初始化监听失败:" + fmt.Sprint(err))
    }
    defer watcher.Close()
    for _, p := range config.Paths {
        if err = watcher.Add(p); err != nil {
            fatal("文件夹 " + p + " 初始化监听失败: " + fmt.Sprint(err))
        }
        logging("文件夹 " + p + " 监听中...  文件类型: " + fmt.Sprint(config.Exts))
    }
    for _, p := range config.Paths {
        filepath.Walk(p, func(file string, info os.FileInfo, err error) error {
            push(file, queue)
            return nil
        })
    }
    for {
        select {
        case event := <-watcher.Events:
            if event.Op&fsnotify.Create == fsnotify.Create {
                time.Sleep(time.Millisecond * time.Duration(16))
                push(event.Name, queue)
            }
        case err := <-watcher.Errors:
            logging("文件夹监听异常: " + fmt.Sprint(err))
        }
    }
}

func push(file string, queue chan<- SendInfo) {
    if len(config.Exts) <= 0 {
        for _, url := range config.Urls {
            queue <- SendInfo{file, url}
        }
    } else {
        for _, ext := range config.Exts {
            if ext == path.Ext(file) {
                for _, url := range config.Urls {
                    queue <- SendInfo{file, url}
                }
                break
            }
        }
    }
}

func upload(queue chan SendInfo) {
    for {
        sendInfo := <-queue
        file, url := sendInfo.file, sendInfo.url
        logging(file + " -> " + url + " 发送中...")
        status, respBody, err := post(file, url)
        if status == 200 && err == nil {
            logging(file + " -> " + url + " 发送成功")
            if n, f := sent[file]; !f || n <= 0 {
                if len(config.Urls) <= 1 {
                    os.Remove(file)
                } else {
                    sent[file] = 1
                }
            } else {
                if len(config.Urls) <= n+1 {
                    delete(sent, file)
                    os.Remove(file)
                } else {
                    sent[file] = n + 1
                }
            }
        } else {
            logging(file + " -> " + url + " 发送失败: " + fmt.Sprint(status, " ", respBody, " ", err))
            time.AfterFunc(time.Duration(config.Retry)*time.Second, func() {
                queue <- sendInfo
            })
        }
    }
}

func post(file string, url string) (status int, respBody string, err error) {
    buf := &bytes.Buffer{}
    bodyWriter := multipart.NewWriter(buf)
    fw, err := bodyWriter.CreateFormFile("file", file[strings.LastIndex(file, "\\")+1:])
    if err != nil {
        return
    }
    fh, err := os.Open(file)
    if err != nil {
        fmt.Println(123)
        return
    }
    defer fh.Close()
    if _, err = io.Copy(fw, fh); err != nil {
        return
    }
    contentType := bodyWriter.FormDataContentType()
    if err = bodyWriter.Close(); err != nil {
        return
    }
    resp, err := http.Post(url, contentType, buf)
    if err != nil {
        return
    }
    defer resp.Body.Close()
    status = resp.StatusCode
    respBodies, err := ioutil.ReadAll(resp.Body)
    respBody = string(respBodies)
    return
}

func logging(s string) {
    log2.Print(s + "\r\n")
    lw := bufio.NewWriter(lh)
    lw.Flush()
    log.Println(s)
}

func fatal(s string) {
    log2.Print(s + "\r\n")
    lw := bufio.NewWriter(lh)
    lw.Flush()
    log.Fatalln(s)
}
