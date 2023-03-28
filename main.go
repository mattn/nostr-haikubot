package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ikawaha/kagome-dict/uni"
	"github.com/mattn/go-haiku"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip19"
)

var (
	tries = []map[string]string{
		{},
		{`[nN]ostr`: `ノストラ`, `[zZ]ap`: `ザップ`, `[jJ]ack`: `ジャック`, `[bB]ot`: `ボット`},
		{`[nN]ostr`: `ノスター`, `[zZ]ap`: `ザップ`, `[jJ]ack`: `ジャック`, `[bB]ot`: `ボット`},
	}

	rs = []string{
		"wss://nostr-relay.nokotaro.com",
		"wss://relay-jp.nostr.wirednet.jp",
		"wss://relay.snort.social",
		"wss://relay.damus.io",
		"wss://relay.nostrich.land",
	}

	reLink = regexp.MustCompile(`\w+://\S+`)
	reTag  = regexp.MustCompile(`#\w+`)

	nsec = os.Getenv("HAIKUBOT_NSEC")

	baseDir string

	dic = uni.Dict()
)

func init() {
	if dir, err := os.Executable(); err != nil {
		log.Fatal(err)
	} else {
		baseDir = filepath.Dir(dir)
	}
}

func postEvent(nsec string, rs []string, id string, content string, tag string) error {
	ev := nostr.Event{}

	var sk string
	if _, s, err := nip19.Decode(nsec); err == nil {
		sk, _ = s.(string)
	} else {
		return err
	}
	if pub, err := nostr.GetPublicKey(sk); err == nil {
		if _, err := nip19.EncodePublicKey(pub); err != nil {
			return err
		}
		ev.PubKey = pub
	} else {
		return err
	}

	ev.Content = "#[0]\n" + content + " " + tag
	ev.CreatedAt = time.Now()
	ev.Kind = nostr.KindTextNote
	ev.Tags = ev.Tags.AppendUnique(nostr.Tag{"e", id, "", "mention"})
	ev.Sign(sk)
	success := 0
	for _, r := range rs {
		relay, err := nostr.RelayConnect(context.Background(), r)
		if err != nil {
			continue
		}
		status, err := relay.Publish(context.Background(), ev)
		relay.Close()
		if err == nil && status != nostr.PublishStatusFailed {
			success++
		}
	}
	if success == 0 {
		return errors.New("failed to publish")
	}
	return nil
}

func normalize(s string) string {
	s = reLink.ReplaceAllString(s, "")
	s = reTag.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

func isHaiku(content string) bool {
	for _, try := range tries {
		s := content
		for k, v := range try {
			s = strings.ReplaceAll(s, k, v)
		}
		if haiku.MatchWithOpt(s, []int{5, 7, 5}, &haiku.Opt{Udic: dic}) {
			return true
		}
	}
	return false
}

func isTanka(content string) bool {
	for _, try := range tries {
		s := content
		for k, v := range try {
			s = strings.ReplaceAll(s, k, v)
		}
		if haiku.MatchWithOpt(s, []int{5, 7, 5, 7, 7}, &haiku.Opt{Udic: dic}) {
			return true
		}
	}
	return false
}

func analyze(ev *nostr.Event) error {
	content := normalize(ev.Content)
	if isHaiku(content) {
		log.Println("MATCHED HAIKU!", content)
		err := postEvent(nsec, rs, ev.ID, content, "#n575")
		if err != nil {
			return err
		}
	}
	if isTanka(content) {
		log.Println("MATCHED TANKA!", content)
		err := postEvent(nsec, rs, ev.ID, content, "#n57577")
		if err != nil {
			return err
		}
	}
	return nil
}

func server() {
	filters := []nostr.Filter{{
		Kinds: []int{1},
	}}

	from := time.Now()
	enc := json.NewEncoder(os.Stdout)

	log.Println("Connecting to relay")
	relay, err := nostr.RelayConnect(context.Background(), "wss://universe.nostrich.land/?lang=ja")
	if err != nil {
		log.Println(err)
		return
	}
	defer relay.Close()

	log.Println("Connected to relay")

	events := make(chan *nostr.Event, 10)
	filters[0].Since = &from

	var wg sync.WaitGroup
	wg.Add(1)
	go func(wg *sync.WaitGroup, events chan *nostr.Event) {
		defer wg.Done()

		log.Println("Start")
	loop:
		for ev := range events {
			enc.Encode(ev)
			if ev == nil {
				break loop
			}
			err = analyze(ev)
			if err != nil {
				log.Println(err)
				continue
			}
			if ev.CreatedAt.After(from) {
				from = ev.CreatedAt.Add(time.Second)
			}
		}
		log.Println("Finish")
	}(&wg, events)

	log.Println("Subscribing events")
	sub := relay.Subscribe(context.Background(), filters)

loop:
	for {
		select {
		case ev := <-sub.Events:
			events <- ev
		case <-time.After(3 * time.Minute):
			log.Println("Timeout")
			sub.Unsub()
			break loop
		}
	}
	wg.Wait()

	log.Println("Stopped")
}

func main() {
	var tt bool
	flag.BoolVar(&tt, "t", false, "test")
	flag.Parse()

	if tt {
		if isHaiku(strings.Join(flag.Args(), " ")) {
			fmt.Println("HAIKU!")
		} else if isTanka(strings.Join(flag.Args(), " ")) {
			fmt.Println("TANKA!")
		}
		return
	}

	if nsec == "" {
		log.Fatal("HAIKUBOT_NSEC is not set")
	}

	for {
		server()
		time.Sleep(5 * time.Second)
	}
}
