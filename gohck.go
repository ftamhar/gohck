package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/jinzhu/configor"
	"github.com/slack-go/slack"
)

var (
	Config = struct {
		// SlackApiKey is the slack api key
		SlackApiKey string `required:"true" env:"SLACK_API_KEY"`

		// SlackChannel is the slack channel to post to
		SlackChannel string `required:"true" env:"SLACK_CHANNEL"`

		// Urls is the urls to check
		Urls []string `required:"true"`

		// delay report check url for not spam (minute)
		DelayReport int `default:"30" env:"DELAY_REPORT"`

		// delay check url (minute)
		DelayCheck int `default:"1" env:"DELAY_CHECK"`

		Timezone string `default:"Asia/Jakarta" env:"TIMEZONE"`

		// jumlah worker
		Worker int `default:"5" env:"WORKER"`
	}{}

	config string
	api    *slack.Client
	mutex  sync.Mutex
	tz     *time.Location
)

func main() {
	flag.StringVar(&config, "c", "config.yml", "config .yml file")
	flag.Parse()

	err := configor.Load(&Config, config)
	if err != nil {
		panic(err)
	}

	tz, err = time.LoadLocation(Config.Timezone)
	if err != nil {
		panic(err)
	}

	fmt.Println("worker:", Config.Worker)
	api = slack.New(Config.SlackApiKey)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)

	var wg sync.WaitGroup
	wg.Add(1)
	go checkUrls(ctx, &wg)

	fmt.Println("Bot running")
	<-ch
	fmt.Println("stoping from checking")
	cancel()

	wg.Wait()
}

func checkUrls(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	t := time.NewTicker(time.Millisecond)
	defer t.Stop()

	mapServerIsDown := make(map[string]int)
	isFirstRun := true
	worker := make(chan struct{}, Config.Worker)

	for {
		select {
		case <-ctx.Done():
			fmt.Println("exiting goroutines")
			return

		case <-t.C:
			if isFirstRun {
				t.Reset(time.Minute * time.Duration(Config.DelayCheck))
				isFirstRun = false
			}

			for _, url := range Config.Urls {
				mutex.Lock()
				if _, ok := mapServerIsDown[url]; ok {
					mutex.Unlock()
					continue
				}
				mutex.Unlock()
				wg.Add(1)
				worker <- struct{}{}
				go checkUrl(ctx, url, worker, mapServerIsDown, wg)
			}
		}
	}
}

func checkUrl(ctx context.Context, url string, wk chan struct{}, mapRetryServerIsDown map[string]int, wg *sync.WaitGroup) {
	defer func() {
		<-wk
		wg.Done()
	}()

	resp, err := http.Get(url)
	if err != nil {
		log.Printf("Error: %s", err)

		mutex.Lock()
		defer mutex.Unlock()
		dur, ok := mapRetryServerIsDown[url]
		if !ok || dur >= Config.DelayReport {
			_, _, err = api.PostMessage(Config.SlackChannel, slack.MsgOptionText(time.Now().In(tz).Format(time.RFC1123)+" - url "+url+" tidak dapat diakses", false))
			if err != nil {
				log.Printf("Error: %s", err)
			}
			if dur >= Config.DelayReport {
				mapRetryServerIsDown[url] = 0
			}
		}

		mapRetryServerIsDown[url]++

		wg.Add(1)
		go func() {
			select {
			case <-ctx.Done():
				wg.Done()
				return
			case <-time.After(time.Minute):
				wk <- struct{}{}
				checkUrl(ctx, url, wk, mapRetryServerIsDown, wg)
			}
		}()

		return
	}
	resp.Body.Close()

	if resp.StatusCode != 200 {
		mutex.Lock()
		defer mutex.Unlock()
		dur, ok := mapRetryServerIsDown[url]
		if !ok || dur >= Config.DelayReport {
			_, _, err = api.PostMessage(Config.SlackChannel, slack.MsgOptionText(time.Now().In(tz).Format(time.RFC1123)+" - url "+url+" meresponse dengan status code: "+strconv.Itoa(resp.StatusCode), false))
			if err != nil {
				log.Printf("Error: %s", err)
			}
			if dur >= Config.DelayReport {
				mapRetryServerIsDown[url] = 0
			}
		}

		mapRetryServerIsDown[url]++

		wg.Add(1)
		go func() {
			select {
			case <-ctx.Done():
				wg.Done()
				return
			case <-time.After(time.Minute):
				wk <- struct{}{}
				checkUrl(ctx, url, wk, mapRetryServerIsDown, wg)
			}
		}()

		return
	}

	mutex.Lock()
	_, ok := mapRetryServerIsDown[url]
	if ok {
		delete(mapRetryServerIsDown, url)
		mutex.Unlock()

		_, _, err := api.PostMessage(Config.SlackChannel, slack.MsgOptionText(time.Now().In(tz).Format(time.RFC1123)+" - url "+url+" sudah dapat diakses kembali", false))
		if err != nil {
			log.Printf("Error: %s", err)
		}
		return
	}
	mutex.Unlock()
}
