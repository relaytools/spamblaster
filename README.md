# spamblaster
Strfry plugin for spam control and curation

## build
```
go get
go build
```
## create spamblaster.cfg
provide a URL to the relay.tools API

example using nostr1.com:

```
https://nostr1.com/api/sconfig/relays/<myRelayID>"
```

## configure [strfry](https://github.com/hoytech/strfry)

in [strfry.conf](https://github.com/hoytech/strfry/blob/master/strfry.conf)

```
writePolicy {
        # If non-empty, path to an executable script that implements the writePolicy plugin logic
        plugin = "/path/to/spamblaster"

        # Number of seconds to search backwards for lookback events when starting the writePolicy plugin (0 for no lookback)
        lookbackSeconds = 0
    }
```

## Features

[x] basic spam prevention using levenshtein distance

[x] relay modes: private, public, allow_list, block_list


