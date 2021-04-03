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
	"bytes"
	"crypto/tls"
	"encoding/json"
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

type QueryConfig struct {
	Headers  map[string]string
	Username string
	Password string
	Body     string
	Url      string
	Method   string
}

func main() {

	cookieFile := flag.String("cookiejar", "", "cookiejar to read from")
	proxyPort := flag.String("port", "8675", "port to listen on")
	proxyHost := flag.String("host", "", "host IP to listen on")
	cookieRefresh := flag.String("refresh", "120", "number of seconds to wait between cookie updates")
	cookieRequest := flag.String("request", "", "JSON blob describing the string to use to grab the cookies")
	flag.Parse()

	cookieQuery := QueryConfig{}
	err := json.Unmarshal([]byte(*cookieRequest), &cookieQuery)
	if err != nil {
		panic(err)
	}

	log.Println("Cookie File: ", *cookieFile)
	log.Println("Port: ", *proxyPort)
	log.Println("Proxy Host: ", *proxyHost)
	log.Println("Command Line Reuqest: ", *cookieRequest)
	log.Println("Request URL: ", cookieQuery.Url)

	cookieRefreshSeconds, err := strconv.ParseInt(*cookieRefresh, 10, 32)
	if err != nil {
		panic(err)
	}
	go cookieService(*cookieFile, int(cookieRefreshSeconds), &cookieQuery)

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

func cookieService(cookieFile string, refresh int, cookieQuery *QueryConfig) {
	/* periodically read the cookie file and update the local cookies */
	if cookieFile == "" && cookieQuery.Url == "" {
		log.Println("No cookie file and no target URL for obtaining cookies")
		return
	}

	for {

		var newCookies []*http.Cookie

		// perform an HTTP query
		if cookieQuery != nil && cookieQuery.Url != "" {

			// FIXME: this should be a flag about whether or not to allow insecure
			// see: https://stackoverflow.com/a/12122718/57626
			tr := &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			}
			client := &http.Client{Transport: tr}

			resp, err := client.Post(cookieQuery.Url, "application/json", bytes.NewBufferString(cookieQuery.Body))
			if err != nil {
				panic(err)
			}
			respCookies := resp.Cookies()
			for i := 0; i < len(respCookies); i++ {
				cookie := respCookies[i]

				// some of the cookies don't come back with a domain
				// this should add those domains into the cookies
				if cookie.Domain == "" {
					u, err := url.Parse(cookieQuery.Url)
					if err != nil {
						panic(err)
					}
					cookie.Domain = u.Hostname()
				}
				// log.Printf("Domain: %s", cookie.Domain)
				// log.Printf("Path: %s", cookie.Path)
				// log.Printf("Name: %s", cookie.Name)
				// log.Printf("Value: %s", cookie.Value)
				// log.Printf("MaxAge: %d", cookie.MaxAge)
				// log.Printf("Expires: %s", cookie.Expires)
				// log.Printf("HTTP Only: %t", cookie.HttpOnly)
				// log.Printf("Secure: %t", cookie.Secure)
				newCookies = append(newCookies, respCookies[i])
			}

			if resp.StatusCode >= 400 {
				log.Printf("WARN: Query to %s returned code %d", cookieQuery.Url,
					resp.StatusCode)
			}
			resp.Body.Close()
		}
		// read in additional cookies from a cookie file
		if cookieFile != "" {
			f, err := os.Open(cookieFile)
			if err != nil {
				log.Fatal(err)
				return
			}

			// FIXME: there is a small chance of a race condition here
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
				newCookies = append(newCookies, cookie)
			}
			log.Printf("loaded %d cookies\n", len(newCookies))

			err = s.Err()
			if err != nil {
				log.Fatal(err)
			}

			f.Close()
		}
		waitgroup.Add(1)
		cookies = newCookies
		waitgroup.Done()

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
