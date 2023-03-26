package main

import (
	"context"
	"encoding/json"
	"errors"
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
		{"nostr": "ノストラ", "zap": "ザップ", "jack": "ジャック"},
		{"nostr": "ノスター", "zap": "ザップ", "jack": "ジャック"},
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

	if nsec == "" {
		log.Fatal("HAIKUBOT_NSEC is not set")
	}
}

func postEvent(nsec string, rs []string, id string, content string) error {
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

	ev.Content = "#[0]\n" + content + " #n575"
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

func analyze(ev *nostr.Event) error {
	content := normalize(ev.Content)
	var id string
	if haiku.MatchWithOpt(content, []int{5, 7, 5}, &haiku.Opt{Udic: dic}) {
		id = ev.ID
	} else {
		for _, try := range tries {
			s := content
			for k, v := range try {
				s = strings.ReplaceAll(s, k, v)
			}
			if haiku.MatchWithOpt(s, []int{5, 7, 5}, &haiku.Opt{Udic: dic}) {
				id = ev.ID
				break
			}
		}
	}
	if id != "" {
		log.Println("MATCH!", content)
		err := postEvent(nsec, rs, id, content)
		if err != nil {
			return err
		}
	}
	return nil
}

func main() {
	relay, err := nostr.RelayConnect(context.Background(), "wss://universe.nostrich.land/?lang=ja")
	if err != nil {
		log.Fatal(err)
	}

	filters := []nostr.Filter{{
		Kinds: []int{1},
		Limit: 10,
	}}

	enc := json.NewEncoder(os.Stdout)
	from := time.Now()
	for {
		ctx, cancel := context.WithCancel(context.Background())

		filters[0].Since = &from
		log.Println("Subscribe events")
		sub := relay.Subscribe(ctx, filters)

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-sub.EndOfStoredEvents
			cancel()
		}()

		for ev := range sub.Events {
			enc.Encode(ev)
			err = analyze(ev)
			if err != nil {
				log.Println(err)
			}
			if ev.CreatedAt.After(from) {
				from = ev.CreatedAt.Add(time.Second)
			}
		}
		wg.Wait()
		time.Sleep(5 * time.Second)
	}
}
