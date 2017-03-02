package main
import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"github.com/eahydra/socks"
	"gopkg.in/alecthomas/kingpin.v2"
)
var (
	proxylist           []*Proxy
	isOn                bool = false
	wg                  sync.WaitGroup
	serverURL, serverIP string
	isProxied           bool  = true
	cur                 int32 = -1
	session             *http.Client
	muteLock            = new(sync.Mutex)
	cid                 = kingpin.Flag("cid", "room number").Required().Short('c').Int()
	path                = kingpin.Flag("filepath", "proxy file path").Default("").Short('p').String()
	retry               = kingpin.Flag("retry", "proxy retry count").Default("5").Short('r').Int()
	multi               = kingpin.Flag("thread_number", "thread number").Required().Short('t').Int()
	detect              = kingpin.Flag("detect", "detection of room").Default("true").Bool()
)
const (
	DOC = `用法:
-t, --thread_number：线程数，即你想刷的人数
-p, --filepath：代理文件
-r, --retry：代理重连次数，默认为5，设置成0为无限
注意：多加一个指令 --no-detect 可以使直播间状态一直检测为开启，即不自动关闭。
按 Ctrl+C 退出`
	SERVICE = `live_number`
	VERSION = `1.5`
)
type Proxy struct {
	host   string
	socks4 *socks.Socks4Client
	socks5 *socks.Socks5Client
}
type Info struct {
	Data struct {
		Status string `json:"_status"`
	} `json:"data"`
}
type Root struct {
	Server string `xml:"server"`
}
func Network(url, method, query, referer string) (*http.Response, error) {
	var err error
	var req *http.Request
	switch method {
	case "GET":
		req, err = http.NewRequest("GET", url, nil)
		req.URL.RawQuery = query
	case "POST":
		req, err = http.NewRequest("POST", url, strings.NewReader(query))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	}
	if err != nil {
		log.Fatal(err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/535.11 (KHTML, like Gecko) Ubuntu/11.10 Chromium/27.0.1453.93 Chrome/27.0.1453.93 Safari/537.36")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	return session.Do(req)
}
func GetServerLink() string {
	var ret *Root
	for {
		if resp, err := Network("http://live.bilibili.com/api/player", "GET", fmt.Sprintf("id=cid:%d", *cid), fmt.Sprintf("http://live.bilibili.com/%d", *cid)); err == nil {
			if body, err := ioutil.ReadAll(resp.Body); err == nil {
				body = append(append([]byte(`<Root>`), body...), []byte(`</Root>`)...)
				if err := xml.Unmarshal(body, &ret); err == nil {
					break
				}
			}
		}
		time.Sleep(5 * time.Second)
	}
	return ret.Server
}
func getNewProxy() *Proxy {
	muteLock.Lock()
	defer muteLock.Unlock()
	give := int(atomic.AddInt32(&cur, 1))
	if give >= len(proxylist) {
		wg.Done()
		return nil
	}
	return proxylist[give]
}
func AddProxy(tem []string) bool {
	out := func() {
		fmt.Println(tem)
		log.Fatal("文件内有错误，请检查！")
	}
	tSize := len(tem)
	if tSize != 2 && !(tSize == 3 && tem[1] == "4") && !(tSize == 4 && tem[1] == "5") {
		out()
	}
	Ina := Proxy{host: tem[0]}
	var user, pass string
	if tSize == 4 {
		user = tem[2]
		pass = tem[3]
	} else if tSize == 3 {
		user = tem[2]
	}
	switch tem[1] {
	case "4":
		Ina.socks4, _ = socks.NewSocks4Client("tcp", Ina.host, user, socks.Direct)
	case "5":
		Ina.socks5, _ = socks.NewSocks5Client("tcp", Ina.host, user, pass, socks.Direct)
	default:
		out()
	}
	for _, i := range proxylist {
		if reflect.DeepEqual(i, &Ina) {
			return false
		}
	}
	proxylist = append(proxylist, &Ina)
	return true
}
func GetProxyList() {
	file, err := os.Open(*path)
	if os.IsNotExist(err) {
		log.Fatal("文件不存在！")
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		AddProxy(strings.Split(scanner.Text(), " "))
	}
	log.Printf("已读取%d个代理……", len(proxylist))
}
func RandInt(min, max int) int {
	return rand.Intn(max-min) + min
}
func Connector(proxy *Proxy, deadCnt int) {
	if deadCnt != 0 {
		if deadCnt >= *retry {
			if proxy != nil {
				log.Printf("%s无法连通！", proxy.host)
			}
			if isProxied {
				go func() {
					if ret := getNewProxy(); ret != nil {
						Connector(ret, 0)
					}
				}()
			} else {
				wg.Done()
			}
			return
		}
		time.Sleep(5 * time.Second)
	}
	DialFuc := net.Dial
	connectHost := serverURL
	if proxy != nil {
		if proxy.socks5 != nil {
			DialFuc = proxy.socks5.Dial
		} else if proxy.socks4 != nil {
			connectHost = serverIP
			DialFuc = proxy.socks4.Dial
		} else {
			os.Exit(1) // WTF
		}
	}
	conn, err := DialFuc("tcp", connectHost+":788")
	if err != nil {
		go Connector(proxy, deadCnt+1)
		return
	}
	cdata := fmt.Sprintf(`{"roomid":%d,"uid":%d}`, *cid, RandInt(1e5, 3.6e7))
	handshake := fmt.Sprintf("%08x001000010000000700000001", len(cdata)+16)
	buf := make([]byte, len(handshake)>>1)
	hex.Decode(buf, []byte(handshake))
	_, err = conn.Write(append(buf, []byte(cdata)...))
	if err != nil {
		go Connector(proxy, 0)
		return
	}
	for {
		buf := make([]byte, 16)
		hex.Decode(buf, []byte("00000010001000010000000200000001"))
		_, err := conn.Write(buf)
		if err != nil {
			go Connector(proxy, 0)
			return
		}
		time.Sleep(30 * time.Second)
	} // should never end
}
func GetRoomStatus() bool {
	if !*detect {
		return true
	}
	var liveInfo Info
	resp, err := Network("http://live.bilibili.com/live/getInfo", "GET", fmt.Sprintf("roomid=%d", *cid), fmt.Sprintf("http://live.bilibili.com/%d", *cid))
	if err != nil {
		time.Sleep(3 * time.Second)
		return GetRoomStatus()
	}
	json.NewDecoder(resp.Body).Decode(&liveInfo)
	return liveInfo.Data.Status == "on"
}
func init() {
	rand.Seed(time.Now().UnixNano())
	kingpin.CommandLine.HelpFlag.Short('h')
	kingpin.UsageTemplate(DOC)
	kingpin.Parse()
	log.Printf("任务开始，目前版本为%s。", VERSION)
	jar, _ := cookiejar.New(nil)
	session = &http.Client{Jar: jar}
	if *path != "" {
		*multi /= 1
	}
	serverURL = GetServerLink()
	ips, err := net.LookupHost(serverURL)
	switch {
	case err != nil:
		fallthrough
	case len(ips) < 1:
		serverIP = serverURL
	default:
		serverIP = ips[0]
	}
	if *path != "" {
		GetProxyList()
	}
	if *retry <= 0 {
		*retry = 1e9
	}
	if !GetRoomStatus() {
		log.Println("正在等待直播间打开……")
	}
}
func Fmain() {
	size := len(proxylist)
	if size == 0 {
		size = 1
		isProxied = false
	}
	log.Println("正在连接……")
	wg.Add(size * *multi)
	if isProxied {
		for i := 0; i < *multi; i++ {
			go func() {
				Connector(getNewProxy(), 0)
			}()
			time.Sleep(100 * time.Microsecond)
		}
	} else {
		for i := 0; i < *multi; i++ {
			go Connector(nil, 0)
		}
	}
	log.Println("连接完毕！")
	wg.Wait()
	log.Println("所有线程已死亡！")
	exit()
}
func main() {
	for {
		nowSta := GetRoomStatus()
		if nowSta != isOn {
			isOn = nowSta
			switch nowSta {
			case true:
				log.Println("直播间已开启！")
				go Fmain()
			case false:
				log.Println("直播间已关闭！")
				exit()
			}
		}
		time.Sleep(time.Duration(RandInt(30, 60)) * time.Second)
	}
}
func exit() {
	log.Println("最后祝您，____，再见。")
	os.Exit(0)
}