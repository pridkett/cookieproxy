package main

/**
 * a simple golang proxy
 *
 * this is a proxy that is designed to send cookies from a curl style cookie jar
 * along with requests. Why would anyone want something like this? Well, it's
 * a long story, but it's because Tesla changed the API on their energy gateway
 * (Powerwall) so it needs cookies for login. Telegraf doesn't easily support
 * those cookies and I didn't want to have to redo everything and lose all of
 * my prior history to make things work.
 *
 */

import (
	"bufio"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

var cookies []*http.Cookie

var waitgroup sync.WaitGroup

func main() {

	cookieFile := flag.String("cookiejar", "", "cookiejar to read from")
	proxyPort := flag.String("port", "8675", "port to listen on")
	proxyHost := flag.String("host", "", "host IP to listen on")
	cookieRefresh := flag.String("refresh", "120", "number of seconds to wait between cookie updates")

	flag.Parse()

	log.Println("Cookie File: ", *cookieFile)
	log.Println("Port: ", *proxyPort)
	log.Println("Proxy Host: ", *proxyHost)

	cookieRefreshSeconds, err := strconv.ParseInt(*cookieRefresh, 10, 32)
	if err != nil {
		panic(err)
	}
	go cookieService(*cookieFile, int(cookieRefreshSeconds))

	http.HandleFunc("/", hello)
	http.HandleFunc("/p/", proxy)
	bind := fmt.Sprintf("%s:%s", *proxyHost, *proxyPort)
	log.Println("CookieProxy is listening on: ", bind)
	err = http.ListenAndServe(bind, nil)
	if err != nil {
		panic(err)
	}
}

func boolCheck(s string) bool {
	if strings.ToLower(s) == "false" {
		return false
	}
	return true
}

func cookieService(cookieFile string, refresh int) {
	/* periodically read the cookie file and update the local cookies */
	if cookieFile == "" {
		return
	}

	for {
		f, err := os.Open(cookieFile)
		if err != nil {
			log.Fatal(err)
			return
		}

		// FIXME: there is a small chance of a race condition here
		waitgroup.Add(1)
		cookies = nil
		s := bufio.NewScanner(f)
		for s.Scan() {
			var line = strings.TrimSpace(s.Text())
			if strings.HasPrefix(line, "#") || len([]rune(line)) == 0 {
				continue
			}
			var splits = strings.Split(s.Text(), "\t")

			expiration, _ := strconv.Atoi(splits[4])
			cookie := &http.Cookie{
				Domain: splits[0],
				Path:   splits[2],
				Secure: boolCheck(splits[3]),
				// TODO: check if this is proper way to manage this
				MaxAge: expiration,
				Name:   splits[5],
				Value:  splits[6],
			}
			cookies = append(cookies, cookie)
		}
		log.Printf("loaded %d cookies\n", len(cookies))
		waitgroup.Done()

		err = s.Err()
		if err != nil {
			log.Fatal(err)
		}

		f.Close()

		time.Sleep(time.Duration(refresh) * time.Second)
	}
}

func hello(res http.ResponseWriter, req *http.Request) {
	waitgroup.Wait()
	fmt.Fprintf(res, "Hello from CookieProxie")
}

func proxy(res http.ResponseWriter, req *http.Request) {
	waitgroup.Wait()

	/* simple function that will fetch single resource and tunnel the response
	   useful for simple txt or image files
	*/
	var target = req.URL.Query().Get("target")
	var method = req.URL.Query().Get("method")

	if len(method) == 0 {
		method = "GET"
	}

	if len(target) != 0 {

		// allow for insecure connections - see https://stackoverflow.com/a/12122718/57626
		tr := &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}

		client := &http.Client{Transport: tr}
		r, err := http.NewRequest(method, target, nil)

		if err != nil {
			io.WriteString(res, "Error Creating New HTTP Request: "+err.Error())
			return
		}

		u, _ := url.Parse(target)
		jar, _ := cookiejar.New(nil)
		jar.SetCookies(u, cookies)
		client.Jar = jar
		log.Printf("target url: %s, cookies sent: %d\n", u, len(jar.Cookies(u)))

		resp, err := client.Do(r)
		if err != nil {
			io.WriteString(res, "Error Sending HTTP Request: "+err.Error())
			return
		}
		defer resp.Body.Close()

		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			io.WriteString(res, "Error Reading Body: "+err.Error())
			return
		}

		// TODO: Need to have it forward all of the appropriate
		// headers back to the client for things like content-type etc
		io.WriteString(res, string(body))
	}
}
