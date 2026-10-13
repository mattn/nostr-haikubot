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

The Docker image is built with the `slimdict` build tag, which embeds a copy of
the ipa-neologd dictionary with the feature columns go-haiku does not use
removed (see `cmd/slimdict`). Detection results are the same and it needs about
400MB less memory. To build it locally:

```
go run ./cmd/slimdict -out neologd-slim.dict
go build -tags slimdict .
```

Without the tag, the dictionary is embedded as it is.

## License

MIT

bep-eng.dic（NVDA 日本語版の辞書） is provided from https://fastapi.metacpan.org/source/MASH/Lingua-JA-Yomi-0.01/lib/Lingua/JA

## Author

Yasuhiro Matsumoto (a.k.a. mattn)
