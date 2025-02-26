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

const version = "0.0.66"

var revision = "HEAD"

var (
	//feedRelay = "wss://universe.nostrich.land/?lang=ja"
	feedRelays = []string{
		"wss://relay-jp.nostr.wirednet.jp",
		"wss://yabu.me",
	}

	postRelays = []string{
		//"wss://nostr-relay.nokotaro.com",
		"wss://relay-jp.nostr.wirednet.jp",
		"wss://yabu.me",
		//"wss://nostr.holybea.com",
		//"wss://relay.snort.social",
		"wss://relay.damus.io",
		//"wss://relay.nostrich.land",
		//"wss://nostr.h3z.jp",
	}

	nsec string
	sk   string
	pub  string

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

	nsec = os.Getenv("HAIKUBOT_NSEC")
	if nsec == "" {
		panic("HAIKUBOT_NSEC is not set")
	}
	if _, s, err := nip19.Decode(nsec); err == nil {
		sk, _ = s.(string)
	} else {
		panic(err.Error())
	}
	if pub, err := nostr.GetPublicKey(sk); err == nil {
		if _, err := nip19.EncodePublicKey(pub); err != nil {
			panic(err.Error())
		}
	} else {
		panic(err.Error())
	}
}

func replyEvent(rs []string, evv *nostr.Event, content string) error {
	ev := nostr.Event{}

	ev.PubKey = pub
	ev.CreatedAt = nostr.Timestamp(evv.CreatedAt.Time().Add(time.Second).Unix())
	ev.Kind = evv.Kind
	ev.Content = content
	ev.Tags = ev.Tags.AppendUnique(nostr.Tag{"e", evv.ID, "", "root"})
	ev.Tags = ev.Tags.AppendUnique(nostr.Tag{"p", evv.PubKey})
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

func postEvent(rs []string, evv *nostr.Event, content string, tag string) error {
	ev := nostr.Event{}

	ev.PubKey = pub
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
		hasRoot := false
		for _, tag := range evv.Tags.FilterOut([]string{"e", "p"}) {
			ev.Tags = ev.Tags.AppendUnique(tag)
			if len(tag) >= 4 && tag[0] == "e" && tag[3] == "root" {
				hasRoot = true
			}
		}
		if !hasRoot {
			ev.Tags = ev.Tags.AppendUnique(nostr.Tag{"e", evv.ID, "", "root"})
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
	if ev.PubKey == pub || strings.Contains(ev.Content, "#n575") || !reJapanese.MatchString(ev.Content) {
		return nil
	}
	content := normalize(ev.Content)
	if isHaiku(content) {
		log.Println("MATCHED HAIKU!", content)
		err := postEvent(postRelays, ev, content, "#n575 #haiku")
		if err != nil {
			return err
		}
	}
	if isTanka(content) {
		log.Println("MATCHED TANKA!", content)
		err := postEvent(postRelays, ev, content, "#n57577 #tanka")
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
		return
	}
	defer resp.Body.Close()
}

func server(from *time.Time) {
	enc := json.NewEncoder(os.Stdout)

	log.Println("Connecting to relay")
	pool := nostr.NewSimplePool(context.Background())

	events := make(chan *nostr.Event, 100)
	timestamp := nostr.Timestamp(from.Unix())
	filters := []nostr.Filter{{
		Kinds: []int{nostr.KindTextNote, nostr.KindChannelMessage},
		Since: &timestamp,
	}}
	sub := pool.SubMany(context.Background(), feedRelays, filters)

	hbtimer := time.NewTicker(5 * time.Minute)
	defer hbtimer.Stop()

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
				if ev.PubKey == pub {
					continue
				}
				for _, v := range ev.Tags {
					if len(v) >= 2 && v[0] == "t" && v[1] == "俳句チェック" {
						s := normalize(ev.Content)
						var buf bytes.Buffer
						haiku.MatchWithOpt(s, []int{5, 7, 5}, &haiku.Opt{Dict: kagomeDic, UserDict: userDic, Debug: true, DebugWriter: &buf})
						err := replyEvent(postRelays, ev, buf.String())
						if err != nil {
							log.Println(err)
						}
						continue events_loop
					}
					if len(v) >= 2 && v[0] == "t" && v[1] == "短歌チェック" {
						s := normalize(ev.Content)
						var buf bytes.Buffer
						haiku.MatchWithOpt(s, []int{5, 7, 5, 7, 7}, &haiku.Opt{Dict: kagomeDic, UserDict: userDic, Debug: true, DebugWriter: &buf})
						err := replyEvent(postRelays, ev, buf.String())
						if err != nil {
							log.Println(err)
						}
						continue events_loop
					}
				}
				err := analyze(ev)
				if err != nil {
					log.Println(err)
					continue
				}
				if ev.CreatedAt.Time().After(*from) {
					*from = ev.CreatedAt.Time()
				}
			case <-hbtimer.C:
				if url := os.Getenv("HEARTBEAT_URL"); url != "" {
					go heartbeatPush(url)
				}
			}
		}
		log.Println("Finish")
	}(&wg, events)

	log.Println("Subscribing events")

loop:
	for {
		ev, ok := <-sub
		if !ok {
			break loop
		}
		events <- ev.Event
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

	from := time.Now()
	for {
		server(&from)
		time.Sleep(5 * time.Second)
	}
}
