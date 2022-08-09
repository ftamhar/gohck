package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
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

		// jumlah worker
		Worker int `default:"5" env:"WORKER"`
	}{}

	config string
	api    *slack.Client
	mutex  sync.Mutex
)

func main() {
	flag.StringVar(&config, "c", "config.yml", "config .yml file")
	flag.Parse()

	err := configor.Load(&Config, config)
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

	mapServerIsShutdown := make(map[string]time.Time)
	mapServerHasChecked := make(map[string]struct{})
	isFirstRun := true
	durReportDelay := time.Duration(Config.DelayReport) * time.Minute
	worker := make(chan struct{}, Config.Worker)

	for {
		select {
		case <-ctx.Done():
			fmt.Println("exiting goroutine")
			return

		case <-t.C:
			if isFirstRun {
				t.Reset(time.Minute * time.Duration(Config.DelayCheck))
				isFirstRun = false
			}

			now := time.Now()
			for _, url := range Config.Urls {

				mutex.Lock()
				e, ok := mapServerIsShutdown[url]
				mutex.Unlock()
				if ok {
					mutex.Lock()
					_, hasChecked := mapServerHasChecked[url]
					if hasChecked {
						mutex.Unlock()
						continue
					}

					mapServerHasChecked[url] = struct{}{}
					mutex.Unlock()

					sub := now.Sub(e) - durReportDelay
					if sub < 0 {
						sub *= -1
					}

					go func(ctx context.Context, url string, dur time.Duration) {
						select {
						case <-ctx.Done():
							return
						case <-time.After(dur):

							worker <- struct{}{}
							checkUrl(url, worker, mapServerIsShutdown)

							time.Sleep(time.Second * 8)
							mutex.Lock()
							delete(mapServerHasChecked, url)
							mutex.Unlock()
							return
						}
					}(ctx, url, sub)

					continue
				}

				worker <- struct{}{}
				go checkUrl(url, worker, mapServerIsShutdown)
			}
		}
	}
}

func checkUrl(url string, wk chan struct{}, mapIfError map[string]time.Time) {
	defer func() {
		<-wk
	}()

	resp, err := http.Get(url)
	if err != nil {
		log.Printf("Error: %s", err)

		_, _, err = api.PostMessage(Config.SlackChannel, slack.MsgOptionText("url "+url+" tidak dapat diakses", false))
		if err != nil {
			log.Printf("Error: %s", err)
		}

		mutex.Lock()
		mapIfError[url] = time.Now()
		mutex.Unlock()
		return
	}
	resp.Body.Close()

	if resp.StatusCode != 200 {
		_, _, err = api.PostMessage(Config.SlackChannel, slack.MsgOptionText("url "+url+" tidak dapat diakses", false))
		if err != nil {
			log.Printf("Error: %s", err)
		}

		mutex.Lock()
		mapIfError[url] = time.Now()
		mutex.Unlock()
		return
	}

	mutex.Lock()
	_, ok := mapIfError[url]
	if ok {
		delete(mapIfError, url)
		mutex.Unlock()

		_, _, err := api.PostMessage(Config.SlackChannel, slack.MsgOptionText("url "+url+" dapat diakses kembali", false))
		if err != nil {
			log.Printf("Error: %s", err)
		}
		return
	}
	mutex.Unlock()
}
