package main

import (
	"context"
	_ "embed"
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

const name = "nostr-haikubot"

const version = "0.0.8"

var revision = "HEAD"

var (
	//feedRelay = "wss://universe.nostrich.land/?lang=ja"
	feedRelay = "wss://relay-jp.nostr.wirednet.jp"

	postRelays = []string{
		"wss://nostr-relay.nokotaro.com",
		"wss://relay-jp.nostr.wirednet.jp",
		"wss://relay.snort.social",
		"wss://relay.damus.io",
		"wss://relay.nostrich.land",
	}

	nsec = os.Getenv("HAIKUBOT_NSEC")

	baseDir string

	unidic = uni.Dict()

	reLink = regexp.MustCompile(`\b\w+://\S+\b`)
	reTag  = regexp.MustCompile(`\B#\S+`)

	//go:embed dict.json
	worddata []byte

	//go:embed bep-eng.dic
	engdata []byte

	words = map[*regexp.Regexp]string{}
)

func init() {
	if dir, err := os.Executable(); err != nil {
		log.Fatal(err)
	} else {
		baseDir = filepath.Dir(dir)
	}

	time.Local = time.FixedZone("Local", 9*60*60)

	var m map[string]string
	if err := json.Unmarshal(worddata, &m); err != nil {
		log.Fatal(err)
	}
	words = make(map[*regexp.Regexp]string)
	for k, v := range m {
		words[regexp.MustCompile(k)] = v
	}
	for _, v := range strings.Split(string(engdata), "\n") {
		if len(v) == 0 || v[0] == '#' {
			continue
		}
		tok := strings.Split(v, " ")
		words[regexp.MustCompile(`\b(?i:`+tok[0]+`)\b`)] = tok[1]
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

func isHaiku(s string) bool {
	for k, v := range words {
		s = k.ReplaceAllString(s, v)
	}
	return haiku.MatchWithOpt(s, []int{5, 7, 5}, &haiku.Opt{Udic: unidic})
}

func isTanka(s string) bool {
	for k, v := range words {
		s = k.ReplaceAllString(s, v)
	}
	return haiku.MatchWithOpt(s, []int{5, 7, 5, 7, 7}, &haiku.Opt{Udic: unidic})
}

func analyze(ev *nostr.Event) error {
	if strings.Contains(ev.Content, "#n575") {
		return nil
	}
	content := normalize(ev.Content)
	if isHaiku(content) {
		log.Println("MATCHED HAIKU!", content)
		err := postEvent(nsec, postRelays, ev.ID, content, "#n575 #haiku")
		if err != nil {
			return err
		}
	}
	if isTanka(content) {
		log.Println("MATCHED TANKA!", content)
		err := postEvent(nsec, postRelays, ev.ID, content, "#n57577 #tanka")
		if err != nil {
			return err
		}
	}
	return nil
}

func server(from *time.Time) {
	enc := json.NewEncoder(os.Stdout)

	log.Println("Connecting to relay")
	relay, err := nostr.RelayConnect(context.Background(), feedRelay)
	if err != nil {
		log.Println(err)
		return
	}
	defer relay.Close()

	log.Println("Connected to relay")

	events := make(chan *nostr.Event, 100)
	filters := []nostr.Filter{{
		Kinds: []int{1},
		Since: from,
	}}
	sub := relay.Subscribe(context.Background(), filters)

	var wg sync.WaitGroup
	wg.Add(1)
	go func(wg *sync.WaitGroup, events chan *nostr.Event) {
		defer wg.Done()

		log.Println("Start")
	events_loop:
		for {
			select {
			case ev, ok := <-events:
				if !ok {
					break events_loop
				}
				enc.Encode(ev)
				err = analyze(ev)
				if err != nil {
					log.Println(err)
					continue
				}
				if ev.CreatedAt.After(*from) {
					*from = ev.CreatedAt
				}
			case <-time.After(10 * time.Second):
				log.Println("Health check")
			}
		}
		log.Println("Finish")
	}(&wg, events)

	log.Println("Subscribing events")

	retry := 0
loop:
	for {
		select {
		case ev, ok := <-sub.Events:
			if !ok || ev == nil {
				break loop
			}
			events <- ev
			retry = 0
		case <-time.After(100 * time.Second):
			retry++
			log.Println("Retrying", retry)
			if retry > 10 {
				close(events)
				break loop
			}
			sub.Filters[0].Since = from
			sub.Fire()
		}
	}
	sub.Unsub()
	wg.Wait()

	log.Println("Stopped")
}

func main() {
	var ver bool
	var tt bool
	flag.BoolVar(&ver, "v", false, "show version")
	flag.BoolVar(&tt, "t", false, "test")
	flag.Parse()

	if ver {
		fmt.Println(version)
		os.Exit(0)
	}

	if tt {
		s := normalize(strings.Join(flag.Args(), " "))
		fmt.Println(s)
		if isHaiku(s) {
			fmt.Println("HAIKU!")
		} else if isTanka(s) {
			fmt.Println("TANKA!")
		}
		return
	}

	if nsec == "" {
		log.Fatal("HAIKUBOT_NSEC is not set")
	}

	from := time.Now()
	for {
		server(&from)
		time.Sleep(5 * time.Second)
	}
}
