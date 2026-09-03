# nostr-haikubot

![nostr-haikubot](https://raw.githubusercontent.com/mattn/nostr-haikubot/main/screenshot.png)

## Usage

```
$ export HAIKUBOT_NSEC=nsecxxxxxxxxxxx...
$ ./nostr-haikubot
```

## Installation

```
go install github.com/mattn/nostr-haikubot@latest
```

`make build` builds with the `slimdict` build tag, which embeds a copy of the
ipa-neologd dictionary with the feature columns go-haiku does not use removed
(see `cmd/slimdict`). Detection results are the same and it needs about 400MB
less memory. The command above, without the tag, embeds the dictionary as it
is.

## License

MIT

bep-eng.dic（NVDA 日本語版の辞書） is provided from https://fastapi.metacpan.org/source/MASH/Lingua-JA-Yomi-0.01/lib/Lingua/JA

## Author

Yasuhiro Matsumoto (a.k.a. mattn)
