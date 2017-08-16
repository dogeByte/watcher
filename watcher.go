package main

import (
    "log"
    "github.com/go-fsnotify/fsnotify"
    "path"
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
)

type Config struct {
    Path  string
    Ext   []string
    Url   string
    Retry uint64
}

var (
    config Config
    log2   *log.Logger
    lh     *os.File
)

func main() {
    defer lh.Close()
    done := make(chan bool)
    queue := make(chan string)
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
        fatal("配置文件加载失败")
    }
    if err = yaml.Unmarshal(configBytes, &config); err != nil {
        fatal("配置文件读取失败")
    }
    if _, err = os.Stat(config.Path); err != nil || os.IsNotExist(err) {
        if err = os.MkdirAll(config.Path, 777); err != nil {
            fatal("文件夹 " + config.Path + " 创建失败，请检查配置文件")
        }
        logging("文件夹 " + config.Path + " 创建成功")
    }
    for k, v := range config.Ext {
        if !strings.HasPrefix(v, ".") {
            config.Ext[k] = "." + v
        }
    }
}

func watch(queue chan<- string) {
    watcher, err := fsnotify.NewWatcher()
    if err != nil {
        fatal("文件夹 " + config.Path + " 初始化监听失败:" + fmt.Sprint(err))
    }
    defer watcher.Close()
    if err = watcher.Add(config.Path); err != nil {
        fatal("文件夹 " + config.Path + " 初始化监听失败: " + fmt.Sprint(err))
    }
    logging("文件夹 " + config.Path + " 监听中...  文件类型: " + fmt.Sprint(config.Ext))
    for {
        select {
        case event := <-watcher.Events:
            if event.Op&fsnotify.Create == fsnotify.Create {
                if len(config.Ext) == 0 {
                    time.Sleep(time.Second)
                    queue <- event.Name
                } else {
                    for _, ext := range config.Ext {
                        if ext == path.Ext(event.Name) {
                            time.Sleep(time.Second)
                            queue <- event.Name
                            break
                        }
                    }
                }
            }
        case err := <-watcher.Errors:
            logging(fmt.Sprint("文件夹 " + config.Path + " 监听异常: " + fmt.Sprint(err)))
        }
    }
}

func upload(queue chan string) {
    for {
        file := <-queue
        status, respBody, err := post(file, config.Url)
        if status == 200 && err == nil {
            logging(file[strings.LastIndex(file, "\\")+1:] + " 发送成功")
            os.Remove(file)
        } else {
            go func() {
                time.Sleep(time.Duration(config.Retry) * time.Second)
                queue <- file
            }()
            logging(file[strings.LastIndex(file, "\\")+1:] + " 发送失败: " + fmt.Sprint(status, " ", respBody, " ", err))
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
