package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/lithammer/fuzzysearch/fuzzy"
	"github.com/nbd-wtf/go-nostr/nip19"
)

// {"event":{"content":"testy testy","created_at":1676015822,"id":"7dcad0a3e8f204a5dd38cec78e37302444f1af5b14e927c5c94d2b00b557f353","kind":1,"pubkey":"eee7c60d4beba24e99eb0e04e77807c09971553ff4199458268e849fe46424eb","sig":"58590143607be25b93493013f8b6513c342e7d184f95f9d76a90d297157ca36cedf92e30da2abfc88af2766f3ab726be2323a53bc678137189e4cc97583c8bd7","tags":[]},"receivedAt":1676015822,"sourceInfo":"127.0.0.1","sourceType":"IP4","type":"new"}
type StrfryEvent struct {
	Event struct {
		Content   string        `json:"content"`
		CreatedAt int           `json:"created_at"`
		ID        string        `json:"id"`
		Kind      int           `json:"kind"`
		Pubkey    string        `json:"pubkey"`
		Sig       string        `json:"sig"`
		Tags      []interface{} `json:"tags"`
	} `json:"event"`
	ReceivedAt int    `json:"receivedAt"`
	SourceInfo string `json:"sourceInfo"`
	SourceType string `json:"sourceType"`
	Type       string `json:"type"`
}

type StrfryResult struct {
	ID     string `json:"id"`     // event id
	Action string `json:"action"` // accept or reject
	Msg    string `json:"msg"`    // sent to client for reject
}

type Relay struct {
	ID                   string      `json:"id"`
	Name                 string      `json:"name"`
	OwnerID              string      `json:"ownerId"`
	Status               interface{} `json:"status"`
	DefaultMessagePolicy bool        `json:"default_message_policy"`
	IP                   interface{} `json:"ip"`
	Capacity             interface{} `json:"capacity"`
	Port                 interface{} `json:"port"`
	AllowList            struct {
		ID           string `json:"id"`
		RelayID      string `json:"relayId"`
		ListKeywords []struct {
			ID          string      `json:"id"`
			AllowListID string      `json:"AllowListId"`
			BlockListID interface{} `json:"BlockListId"`
			Keyword     string      `json:"keyword"`
			Reason      string      `json:"reason"`
			ExpiresAt   interface{} `json:"expires_at"`
		} `json:"list_keywords"`
		ListPubkeys []struct {
			ID          string      `json:"id"`
			AllowListID string      `json:"AllowListId"`
			BlockListID interface{} `json:"BlockListId"`
			Pubkey      string      `json:"pubkey"`
			Reason      string      `json:"reason"`
			ExpiresAt   interface{} `json:"expires_at"`
		} `json:"list_pubkeys"`
	} `json:"allow_list"`
	BlockList struct {
		ID           string `json:"id"`
		RelayID      string `json:"relayId"`
		ListKeywords []struct {
			ID          string      `json:"id"`
			AllowListID interface{} `json:"AllowListId"`
			BlockListID string      `json:"BlockListId"`
			Keyword     string      `json:"keyword"`
			Reason      string      `json:"reason"`
			ExpiresAt   interface{} `json:"expires_at"`
		} `json:"list_keywords"`
		ListPubkeys []struct {
			ID          string      `json:"id"`
			AllowListID interface{} `json:"AllowListId"`
			BlockListID string      `json:"BlockListId"`
			Pubkey      string      `json:"pubkey"`
			Reason      string      `json:"reason"`
			ExpiresAt   interface{} `json:"expires_at"`
		} `json:"list_pubkeys"`
	} `json:"block_list"`
	Owner struct {
		ID     string      `json:"id"`
		Pubkey string      `json:"pubkey"`
		Name   interface{} `json:"name"`
	} `json:"owner"`
	Moderators []struct {
		ID      string `json:"id"`
		RelayID string `json:"relayId"`
		UserID  string `json:"userId"`
	} `json:"moderators"`
}

/*
type Relay struct {
	ID                   string      `json:"id"`
	Name                 string      `json:"name"`
	OwnerID              string      `json:"ownerId"`
	Status               interface{} `json:"status"`
	IP                   interface{} `json:"ip"`
	Capacity             interface{} `json:"capacity"`
	Port                 interface{} `json:"port"`
	DefaultMessagePolicy bool        `json:"default_message_policy"`
	AllowList            struct {
		ID           string `json:"id"`
		RelayID      string `json:"relayId"`
		ListKeywords []struct {
			ID          string      `json:"id"`
			AllowListID string      `json:"whiteListId"`
			BlockListID interface{} `json:"blackListId"`
			Keyword     string      `json:"keyword"`
			Reason      string      `json:"reason"`
			ExpiresAt   interface{} `json:"expires_at"`
		} `json:"list_keywords"`
		ListPubkeys []struct {
			ID          string      `json:"id"`
			WhiteListID string      `json:"whiteListId"`
			BlackListID interface{} `json:"blackListId"`
			Pubkey      string      `json:"pubkey"`
			Reason      interface{} `json:"reason"`
			ExpiresAt   interface{} `json:"expires_at"`
		} `json:"list_pubkeys"`
	} `json:"white_list"`
	BlockList struct {
		ID           string `json:"id"`
		RelayID      string `json:"relayId"`
		ListKeywords []struct {
			ID          string      `json:"id"`
			WhiteListID interface{} `json:"whiteListId"`
			BlackListID string      `json:"blackListId"`
			Keyword     string      `json:"keyword"`
			Reason      string      `json:"reason"`
			ExpiresAt   interface{} `json:"expires_at"`
		} `json:"list_keywords"`
		ListPubkeys []struct {
			ID          string      `json:"id"`
			WhiteListID interface{} `json:"whiteListId"`
			BlackListID string      `json:"blackListId"`
			Pubkey      string      `json:"pubkey"`
			Reason      string      `json:"reason"`
			ExpiresAt   interface{} `json:"expires_at"`
		} `json:"list_pubkeys"`
	} `json:"black_list"`
	Owner struct {
		ID     string      `json:"id"`
		Pubkey string      `json:"pubkey"`
		Name   interface{} `json:"name"`
	} `json:"owner"`
	Moderators []interface{} `json:"moderators"`
}
*/

func expireSeen(seen map[string]time.Time) map[string]time.Time {
	var newSeen = make(map[string]time.Time)
	for k, v := range seen {
		expires := v.Add(3 * time.Hour)
		//log(fmt.Sprintf("\nseen: %s\n%s\n%s\n%s\n\n", k, v, tenMin, time.Now()))
		if time.Now().Before(expires) {
			newSeen[k] = v
		}
	}
	return newSeen
}

var errlog = bufio.NewWriter(os.Stderr)

var logfile *os.File

func initLogging() {
	var err error
	logfile, err = os.OpenFile("spamblaster.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log(err.Error())
	}
}

func logFile(message string) {
	//if _, err := logfile.WriteString(message); err != nil {
	//	log("error writing to file: " + err.Error())
	//}
	log(message)
}

func log(message string) {
	errlog.WriteString(fmt.Sprintln(message))
	errlog.Flush()
}

func compareSimilar(s1 string, s2 string) (float64, bool) {
	l1 := float64(len(s1))
	l2 := float64(len(s2))
	if l1 == 0 || l2 == 0 {
		return 1.00, false
	}
	var pDist float64
	if l1 >= l2 {
		// sizes are different enough
		diffSize := l2 / l1
		if diffSize < 0.7 {
			return 1.00, false
		}
		dist := fuzzy.LevenshteinDistance(s1, s2)
		pDist = float64(dist) / l2
	} else {
		// sizes are different enough
		diffSize := l1 / l2
		if diffSize < 0.7 {
			return 1.00, false
		}
		dist := fuzzy.LevenshteinDistance(s1, s2)
		pDist = float64(dist) / l1
	}
	if pDist < 0.04 {
		return pDist, true
	} else {
		return pDist, false
	}
}

func queryRelay(oldrelay Relay) (Relay, error) {

	relay := Relay{}

	//url := "http://172.17.0.1:3000/api/sconfig/relays/clfpg8rgc0001gh2ot0qdkavd"
	url := "http://172.17.0.1:3000/api/sconfig/relays/clj9061480003ghacbub9mley"

	body, err := ioutil.ReadFile("./spamblaster.cfg")
	if err != nil {
		log(fmt.Sprintf("unable to read config file: %v", err))
	} else {
		url = strings.TrimSuffix(string(body), "\n")
	}

	rClient := http.Client{
		Timeout: time.Second * 5,
	}

	req, err := http.NewRequest(http.MethodGet, url, nil)

	if err != nil {
		log(err.Error())
		return oldrelay, err
	}
	res, getErr := rClient.Do(req)
	if getErr != nil {
		log(getErr.Error())
		return oldrelay, err
	}
	if res.Body != nil {
		defer res.Body.Close()
	}

	body, readErr := ioutil.ReadAll(res.Body)
	if readErr != nil {
		log(readErr.Error())
	}
	jsonErr := json.Unmarshal(body, &relay)
	if jsonErr != nil {
		log("json not unmarshaled")
	}

	//log(fmt.Sprintf("%v", relay))
	return relay, nil
}

func main() {
	initLogging()
	defer logfile.Close()

	var reader = bufio.NewReader(os.Stdin)
	var output = bufio.NewWriter(os.Stdout)
	defer output.Flush()
	defer errlog.Flush()

	var seen = make(map[string]time.Time)

	var err1 error
	var relay Relay
	relay, err1 = queryRelay(relay)
	if err1 != nil {
		log("there was an error fetching relay, using cache or nil")
	}

	ticker := time.NewTicker(5 * time.Second)
	go func() {
		for {
			<-ticker.C
			relay, err1 = queryRelay(relay)
			if err1 != nil {
				log("there was an error fetching relay, using cache or nil")
			}
		}
	}()

	for {
		seen = expireSeen(seen)
		var input, _ = reader.ReadString('\n')
		log(fmt.Sprintf("invoked spamblaster -> seen cache size: %d", len(seen)))

		var e StrfryEvent
		if err := json.Unmarshal([]byte(input), &e); err != nil {
			panic(err)
		}

		result := StrfryResult{
			ID:     e.Event.ID,
			Action: "accept",
		}

		allowMessage := false
		if relay.DefaultMessagePolicy {
			log("policy default: allowing all")
			allowMessage = true
		} else {
			log("policy default: denying all")
		}
		isWl := false
		badResp := ""

		// pubkeys logic
		// false is deny, true is allow
		if !relay.DefaultMessagePolicy {
			// relay is in whitelist pubkey mode, only allow these pubkeys to post
			for _, k := range relay.AllowList.ListPubkeys {
				if strings.Contains(k.Pubkey, "npub") {
					if _, v, err := nip19.Decode(k.Pubkey); err == nil {
						pub := v.(string)
						if strings.Contains(e.Event.Pubkey, pub) {
							log("allowing whitelist for pubkey: " + k.Pubkey)
							allowMessage = true
							isWl = true
						}
					} else {
						log("error decoding pubkey: " + k.Pubkey + " " + err.Error())
					}
				}

				if strings.Contains(e.Event.Pubkey, k.Pubkey) {
					log("allowing whitelist for pubkey: " + k.Pubkey)
					allowMessage = true
					isWl = true
				}
			}
		}

		if relay.BlockList.ListPubkeys != nil && len(relay.BlockList.ListPubkeys) >= 1 {
			// relay is in blacklist pubkey mode, mark bad
			for _, k := range relay.BlockList.ListPubkeys {
				if strings.Contains(k.Pubkey, "npub") {
					if _, v, err := nip19.Decode(k.Pubkey); err == nil {
						pub := v.(string)
						if strings.Contains(e.Event.Pubkey, pub) {
							log("rejecting for pubkey: " + k.Pubkey)
							badResp = "blocked pubkey " + k.Pubkey + " reason: " + k.Reason
							allowMessage = false
							isWl = false
						}
					} else {
						log("error decoding pubkey: " + k.Pubkey + " " + err.Error())
					}
				}
				if strings.Contains(e.Event.Pubkey, k.Pubkey) {
					log("rejecting for pubkey: " + k.Pubkey)
					badResp = "blocked pubkey " + k.Pubkey + " reason: " + k.Reason
					allowMessage = false
				}
			}
		}

		// keywords logic
		if relay.AllowList.ListKeywords != nil && len(relay.AllowList.ListKeywords) >= 1 && !relay.DefaultMessagePolicy {
			// relay has whitelist keywords, allow  messages matching any of these keywords to post, deny messages that don't.
			for _, k := range relay.AllowList.ListKeywords {
				if strings.Contains(e.Event.Content, k.Keyword) {
					log("allowing for keyword: " + k.Keyword)
					allowMessage = true
				}
			}
		}

		if relay.BlockList.ListKeywords != nil && len(relay.BlockList.ListKeywords) >= 1 {
			// relay has blacklist keywords, deny messages matching any of these keywords to post
			for _, k := range relay.BlockList.ListKeywords {
				if strings.Contains(e.Event.Content, k.Keyword) {
					log("rejecting for keyword: " + k.Keyword)
					badResp = "blocked. " + k.Keyword + " reason: " + k.Reason
					allowMessage = false
				}
			}
		}

		seenDist := 0.00
		if allowMessage {
			// spam duplicate inhibitor
			for i := range seen {
				dist, tooSimilar := compareSimilar(i, e.Event.Content)
				// block unless pubkey is specifically whitelisted
				if tooSimilar && !isWl {
					allowMessage = false
					badResp = "blocked. reason: duplicate message"
					seenDist = dist
				}
			}
		}

		// message
		if e.Event.Kind == 1 {
			if !allowMessage {
				result.Action = "reject"
				result.Msg = badResp
				logFile(fmt.Sprintf("blocked,%.2f,%s,%s,%s,%s\n", seenDist, e.SourceInfo, e.Event.Pubkey, e.Event.Content, time.Now()))
			} else {
				logFile(fmt.Sprintf("message,%s,%s,%s\n", e.SourceInfo, e.Event.Pubkey, e.Event.Content))

				if len(e.Event.Content) > 20 && !isWl {
					seen[e.Event.Content] = time.Now()
				}
			}
		}

		// channel message
		if e.Event.Kind == 42 {
			if !allowMessage {
				result.Action = "reject"
				result.Msg = badResp
				logFile(fmt.Sprintf("blocked,%.2f,%s,%s,%s,%s\n", seenDist, e.SourceInfo, e.Event.Pubkey, e.Event.Content, time.Now()))
			} else {
				logFile(fmt.Sprintf("cmessage,%s,%s,%s\n", e.SourceInfo, e.Event.Pubkey, e.Event.Content))
				if len(e.Event.Content) > 20 && !isWl {
					seen[e.Event.Content] = time.Now()
				}
			}
		}

		r, _ := json.Marshal(result)
		output.WriteString(fmt.Sprintf("%s\n", r))
		output.Flush()
	}

}
