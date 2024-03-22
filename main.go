package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	//"github.com/ikawaha/kagome-dict/uni"
	//"github.com/ikawaha/kagome-dict/ipa"
	"github.com/ikawaha/kagome-dict-ipa-neologd"
	"github.com/ikawaha/kagome-dict/dict"
	"github.com/mattn/go-haiku"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip19"
)

const name = "nostr-haikubot"

const version = "0.0.48"

var revision = "HEAD"

var (
	//feedRelay = "wss://universe.nostrich.land/?lang=ja"
	feedRelay = "wss://relay-jp.nostr.wirednet.jp"

	postRelays = []string{
		"wss://nostr-relay.nokotaro.com",
		"wss://relay-jp.nostr.wirednet.jp",
		"wss://nostr.holybea.com",
		"wss://relay.snort.social",
		"wss://relay.damus.io",
		"wss://relay.nostrich.land",
		"wss://nostr.h3z.jp",
	}

	nsec = os.Getenv("HAIKUBOT_NSEC")

	reLink     = regexp.MustCompile(`\b\w+://\S+\b`)
	reTag      = regexp.MustCompile(`\B#\S+`)
	reJapanese = regexp.MustCompile(`[０-９Ａ-Ｚａ-ｚぁ-ゖァ-ヾ一-鶴]`)

	debug = false

	kagomeDic = ipaneologd.Dict()

	//go:embed userdic.txt
	dicdata []byte

	userDic *dict.UserDict
)

func init() {
	time.Local = time.FixedZone("Local", 9*60*60)

	r, err := dict.NewUserDicRecords(bytes.NewReader(dicdata))
	if err != nil {
		panic(err.Error())
	}
	userDic, err = r.NewUserDict()
	if err != nil {
		panic(err.Error())
	}
}

func postEvent(nsec string, rs []string, evv *nostr.Event, content string, tag string) error {
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

	ev.CreatedAt = nostr.Timestamp(evv.CreatedAt.Time().Add(time.Second).Unix())
	ev.Kind = evv.Kind
	if ev.Kind == nostr.KindTextNote {
		if nevent, err := nip19.EncodeEvent(evv.ID, rs, evv.PubKey); err == nil {
			ev.Content = content + " " + tag + "\nnostr:" + nevent
		} else {
			ev.Content = "#[0]\n" + content + " " + tag
		}
		ev.Tags = ev.Tags.AppendUnique(nostr.Tag{"e", evv.ID, "", "mention"})
	} else {
		ev.Content = content + " " + tag
		ev.Tags = ev.Tags.AppendUnique(nostr.Tag{"e", evv.ID, "", "reply"})
		for _, tag := range evv.Tags.FilterOut([]string{"e", "p"}) {
			ev.Tags = ev.Tags.AppendUnique(tag)
		}
	}
	ev.Sign(sk)
	success := 0
	for _, r := range rs {
		relay, err := nostr.RelayConnect(context.Background(), r)
		if err != nil {
			continue
		}
		if relay.Publish(context.Background(), ev) == nil {
			success++
		}
		relay.Close()
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
	return haiku.MatchWithOpt(s, []int{5, 7, 5}, &haiku.Opt{Dict: kagomeDic, UserDict: userDic, Debug: debug})
}

func isTanka(s string) bool {
	return haiku.MatchWithOpt(s, []int{5, 7, 5, 7, 7}, &haiku.Opt{Dict: kagomeDic, UserDict: userDic, Debug: debug})
}

func analyze(ev *nostr.Event) error {
	if strings.Contains(ev.Content, "#n575") || !reJapanese.MatchString(ev.Content) {
		return nil
	}
	content := normalize(ev.Content)
	if isHaiku(content) {
		log.Println("MATCHED HAIKU!", content)
		err := postEvent(nsec, postRelays, ev, content, "#n575 #haiku")
		if err != nil {
			return err
		}
	}
	if isTanka(content) {
		log.Println("MATCHED TANKA!", content)
		err := postEvent(nsec, postRelays, ev, content, "#n57577 #tanka")
		if err != nil {
			return err
		}
	}
	return nil
}

func heartbeatPush(url string) {
	resp, err := http.Get(url)
	if err != nil {
		log.Println(err.Error())
	}
	defer resp.Body.Close()
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
	timestamp := nostr.Timestamp(from.Unix())
	filters := []nostr.Filter{{
		Kinds: []int{nostr.KindTextNote, nostr.KindChannelMessage},
		Since: &timestamp,
	}}
	sub, err := relay.Subscribe(context.Background(), filters)
	if err != nil {
		log.Println(err)
		return
	}

	hbtimer := time.NewTicker(5 * time.Minute)
	defer hbtimer.Stop()

	var wg sync.WaitGroup
	wg.Add(1)
	go func(wg *sync.WaitGroup, events chan *nostr.Event) {
		defer wg.Done()

		retry := 0
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
				if ev.CreatedAt.Time().After(*from) {
					*from = ev.CreatedAt.Time()
				}
				retry = 0
			case <-hbtimer.C:
				if url := os.Getenv("HEARTBEAT_URL"); url != "" {
					go heartbeatPush(url)
				}
			case <-time.After(10 * time.Second):
				if relay.ConnectionError != nil {
					log.Println(err)
					close(events)
					sub.Unsub()
					break events_loop
				}
				retry++
				log.Println("Health check", retry)
				if retry > 60 {
					close(events)
					sub.Unsub()
					break events_loop
				}
			}
		}
		log.Println("Finish")
	}(&wg, events)

	log.Println("Subscribing events")

loop:
	for {
		ev, ok := <-sub.Events
		if !ok || ev == nil {
			break loop
		}
		events <- ev
	}
	wg.Wait()

	log.Println("Stopped")
}

func main() {
	var ver bool
	var tt bool
	flag.BoolVar(&debug, "V", false, "verbose")
	flag.BoolVar(&ver, "version", false, "show version")
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
