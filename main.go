package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"
	"github.com/nbd-wtf/go-nostr/nip19"
	"github.com/spf13/viper"
)

// Strfry Events (FROM STDIN)
type StrfryEvent struct {
	Event struct {
		Content   string     `json:"content"`
		CreatedAt int        `json:"created_at"`
		ID        string     `json:"id"`
		Kind      int        `json:"kind"`
		Pubkey    string     `json:"pubkey"`
		Sig       string     `json:"sig"`
		Tags      [][]string `json:"tags"`
	} `json:"event"`
	ReceivedAt int    `json:"receivedAt"`
	SourceInfo string `json:"sourceInfo"`
	SourceType string `json:"sourceType"`
	Type       string `json:"type"`
}

// Strfry Actions
type StrfryResult struct {
	ID     string `json:"id"`     // event id
	Action string `json:"action"` // accept or reject
	Msg    string `json:"msg"`    // sent to client for reject
}

// Relay Creator Schema
type Relay struct {
	ID                   string `json:"id"`
	Name                 string `json:"name"`
	OwnerID              string `json:"ownerId"`
	DefaultMessagePolicy bool   `json:"default_message_policy"`
	AllowGiftwrap        bool   `json:"allow_giftwrap"`
	AllowTagged          bool   `json:"allow_tagged"`
	AllowKeywordPubkey   bool   `json:"allow_keyword_pubkey"`
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
		ListKinds []struct {
			ID          string      `json:"id"`
			AllowListID string      `json:"AllowListId"`
			BlockListID interface{} `json:"BlockListId"`
			Kind        int         `json:"kind"`
			Reason      string      `json:"reason"`
		} `json:"list_kinds"`
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
		ListKinds []struct {
			ID          string      `json:"id"`
			AllowListID string      `json:"AllowListId"`
			BlockListID interface{} `json:"BlockListId"`
			Kind        int         `json:"kind"`
			Reason      string      `json:"reason"`
		} `json:"list_kinds"`
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
		User    struct {
			Pubkey string `json:"pubkey"`
		} `json:"user"`
	} `json:"moderators"`
}

var errlog = bufio.NewWriter(os.Stderr)

var logfile *os.File

func logFile(message string) {
	log(message)
}

func log(message string) {
	errlog.WriteString(fmt.Sprintln(message))
	errlog.Flush()
}

func decodePub(pubkey string) string {
	usepub := pubkey
	if strings.Contains(pubkey, "npub") {
		if _, v, err := nip19.Decode(pubkey); err == nil {
			usepub = v.(string)
		}
	}
	return usepub
}

func queryRelay(oldrelay Relay) (Relay, error) {

	relay := Relay{}

	// example spamblaster config
	url := "http://127.0.0.1:3000/api/sconfig/relays/clkklcjon000wgh31mcgbut40"

	body, err := os.ReadFile("./spamblaster.cfg")
	if err != nil {
		log(fmt.Sprintf("unable to read config file: %v", err))
	} else {
		url = strings.TrimSuffix(string(body), "\n")
	}

	rClient := http.Client{
		Timeout: time.Second * 10,
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

	body, readErr := io.ReadAll(res.Body)
	if readErr != nil {
		log(readErr.Error())
		return oldrelay, readErr
	}
	jsonErr := json.Unmarshal(body, &relay)
	if jsonErr != nil {
		log("json not unmarshaled")
		return oldrelay, jsonErr
	}

	return relay, nil
}

func isModAction(relay Relay, e StrfryEvent) bool {
	isModAction := false
	for _, m := range relay.Moderators {
		usepub := decodePub(m.User.Pubkey)
		if usepub == e.Event.Pubkey {
			isModAction = true
		}
	}
	if relay.Owner.Pubkey == e.Event.Pubkey {
		isModAction = true
	}
	return isModAction
}

type influxdbConfig struct {
	Url         string `mapstructure:"INFLUXDB_URL"`
	Token       string `mapstructure:"INFLUXDB_TOKEN"`
	Org         string `mapstructure:"INFLUXDB_ORG"`
	Bucket      string `mapstructure:"INFLUXDB_BUCKET"`
	Measurement string `mapstructure:"INFLUXDB_MEASUREMENT"`
}

func main() {
	defer logfile.Close()

	var reader = bufio.NewReader(os.Stdin)
	var output = bufio.NewWriter(os.Stdout)
	defer output.Flush()
	defer errlog.Flush()

	var err1 error
	var relay Relay
	relay, err1 = queryRelay(relay)
	if err1 != nil {
		log("there was an error fetching relay, using cache or nil: " + err1.Error())
	}

	ticker := time.NewTicker(60 * time.Second)
	go func() {
		for {
			<-ticker.C
			relay, err1 = queryRelay(relay)
			if err1 != nil {
				log("there was an error fetching relay, using cache or nil" + err1.Error())
			}
		}
	}()

	// InfluxDB optional config loading
	viper.AddConfigPath("/usr/local/etc")
	viper.SetConfigName(".spamblaster.env")
	viper.SetConfigType("env")
	influxEnabled := true
	var iConfig *influxdbConfig
	if err := viper.ReadInConfig(); err != nil {
		log(fmt.Sprint("Warn: error reading influxdb config file /usr/local/etc/.spamblaster.env\n", err))
		influxEnabled = false
	}
	// Viper unmarshals the loaded env variables into the struct
	if err := viper.Unmarshal(&iConfig); err != nil {
		log(fmt.Sprint("Warn: unable to decode influxdb config into struct\n", err))
		influxEnabled = false
	}

	log(fmt.Sprintf("Info: influxdb: %t\n", influxEnabled))

	var client influxdb2.Client
	var writeAPI api.WriteAPI

	if influxEnabled {
		// INFLUX INIT
		client = influxdb2.NewClientWithOptions(iConfig.Url, iConfig.Token,
			influxdb2.DefaultOptions().SetBatchSize(20))
		// Get non-blocking write client
		writeAPI = client.WriteAPI(iConfig.Org, iConfig.Bucket)
	}

	for {
		var input, _ = reader.ReadString('\n')

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
			allowMessage = true
		}
		badResp := ""

		// moderation retroactive delete
		if e.Event.Kind == 1984 || (e.Event.Kind == 7 && (e.Event.Content == "❌" || e.Event.Content == "🔨")) {

			if isModAction(relay, e) {

				thisReason := ""
				thisEvent := ""
				thisPubkey := ""
				thisAction := ""

				if e.Event.Kind == 1984 {
					log(fmt.Sprintf("1984 request from %s>", e.Event.Pubkey))
					for _, x := range e.Event.Tags {
						if x[0] == "e" {
							thisEvent = x[1]
							thisReason = "mod action by " + e.Event.Pubkey + ": delete event"
						}
					}
					for _, x := range e.Event.Tags {
						if x[0] == "p" {
							thisPubkey = x[1]
							thisReason = "mod action by " + e.Event.Pubkey + ": block and delete pubkey"
						}
					}
					if thisEvent != "" {
						thisAction = "deleteEvent"
					} else if thisPubkey != "" {
						thisAction = "blockAndDeletePubkey"
					}
				} else if e.Event.Kind == 7 && e.Event.Content == "❌" {
					// delete event
					for _, x := range e.Event.Tags {
						if x[0] == "e" {
							thisEvent = x[1]
							thisReason = "mod action by " + e.Event.Pubkey + ": delete event"
						}
					}
					if thisEvent != "" {
						thisAction = "deleteEvent"
					}
				} else if e.Event.Kind == 7 && e.Event.Content == "🔨" {
					// delete the events related to this pubkey
					for _, x := range e.Event.Tags {
						if x[0] == "p" {
							thisPubkey = x[1]
							thisReason = "mod action by " + e.Event.Pubkey + ": block and delete pubkey"
						}
					}
					if thisPubkey != "" {
						thisAction = "blockAndDeletePubkey"
					}
				}

				if thisAction == "deleteEvent" {
					log(fmt.Sprintf("received action from mod: delete event <%s>, reason: %s", thisEvent, thisReason))
					// shell out
					filter := fmt.Sprintf("{\"ids\": [\"%s\"]}", thisEvent)
					cmd := exec.Command("/app/strfry", "delete", "--filter", filter)
					out, err := cmd.Output()
					if err != nil {
						log(fmt.Sprintln("could not run command: ", err))
					}
					log(fmt.Sprintln("strfry command output: ", string(out)))
				} else if thisAction == "blockAndDeletePubkey" {
					log(fmt.Sprintf("received action from mod: block and delete pubkey <%s>, reason: %s", thisPubkey, thisReason))
					// shell out
					filter := fmt.Sprintf("{\"authors\": [\"%s\"]}", thisPubkey)
					cmd := exec.Command("/app/strfry", "delete", "--filter", filter)
					out, err := cmd.Output()
					if err != nil {
						log(fmt.Sprintln("could not run command: ", err))
					}
					log(fmt.Sprintln("strfry command output: ", string(out)))
				}

				// don't publish mod actions, use shadowReject to silently drop them.
				result.Action = "shadowReject"
				r, _ := json.Marshal(result)
				output.WriteString(fmt.Sprintf("%s\n", r))
				output.Flush()
				continue
			}
		}

		// pubkeys logic
		// false is deny, true is allow
		if !relay.DefaultMessagePolicy {
			// relay is in whitelist pubkey mode, only allow these pubkeys to post
			for _, k := range relay.AllowList.ListPubkeys {
				usekey := k.Pubkey
				if strings.Contains(k.Pubkey, "npub") {
					if _, v, err := nip19.Decode(k.Pubkey); err == nil {
						usekey = v.(string)
					} else {
						log("error decoding pubkey: " + k.Pubkey + " " + err.Error())
					}
				}

				// TODO: this is not the best way to match the pubkey
				// account for blank string here at least
				if strings.Contains(e.Event.Pubkey, usekey) {
					log("allowing whitelist for pubkey: " + usekey)
					allowMessage = true
				}

				// if we're allowing tags, check if pubkey is tagged in the messages ptags
				if relay.AllowTagged {
					if e.Event.Tags != nil && len(e.Event.Tags) >= 1 {
						for _, x := range e.Event.Tags {
							if x[0] == "p" {
								if x[1] == usekey {
									log("allowing whitelist for tagged pubkey: " + usekey)
									allowMessage = true
								}
							}
						}
					}
				}
			}
		}

		// allow keywords logic
		if relay.AllowList.ListKeywords != nil && len(relay.AllowList.ListKeywords) >= 1 && !relay.DefaultMessagePolicy {
			// relay has whitelist keywords, allow  messages matching any of these keywords to post, deny messages that don't.
			// If they're allow_listed pubkey, we check the setting for allow_keyword_pubkey.
			// If allow_keyword_pubkey is 'true' still want to obey the keyword list here and only allow the keywords.
			// Else if allow_keyword_pubkey is 'false' we will allow the message if it matches the keyword list.
			foundKeyword := false
			for _, k := range relay.AllowList.ListKeywords {
				dEvent := strings.ToLower(e.Event.Content)
				dKeyword := strings.ToLower(k.Keyword)
				if strings.Contains(dEvent, dKeyword) {
					log("found keyword: " + k.Keyword)
					foundKeyword = true
				}
			}
			log(fmt.Sprintf("allow_keyword_pubkey: %t", relay.AllowKeywordPubkey))

			if relay.AllowKeywordPubkey {
				if foundKeyword && (allowMessage || isModAction(relay, e)) {
					log("allow_keyword_pubkey=true, allowMessage=true, allowing for BOTH")
					allowMessage = true
				} else {
					log("allow_keyword_pubkey=true, keyword AND pubkey not found, deny")
					allowMessage = false
				}
			} else {
				if foundKeyword {
					log("allow_keyword_pubkey=false, pubkey allowed OR keyword allowed, allow")
					allowMessage = true
				}
				// mod allowance check is required here, in keyword mode with allow_keyword_pubkey set to false
				if isModAction(relay, e) {
					log("allowing for mod: " + e.Event.Pubkey)
					allowMessage = true
				}
			}
			// The one specific case you wouldn't want to allow owner+mods is in this AllowList keywords mode
			// Therefor, we will do the mod detector check here and allow all owners+mods for non keyword mode
		} else {
			// allow owners + moderators
			if isModAction(relay, e) {
				log("allowing for mod: " + e.Event.Pubkey)
				allowMessage = true
			}
		}

		// if relay is in Deny mode, and message was being blocked by the ACLs above this, we need to check if the kind is in the allow list, and allow it
		if !relay.DefaultMessagePolicy {
			if relay.AllowList.ListKinds != nil && len(relay.AllowList.ListKinds) >= 1 {
				if !allowMessage {
					for _, k := range relay.AllowList.ListKinds {
						if e.Event.Kind == k.Kind {
							log("allowing for kind: " + fmt.Sprintf("%d", k.Kind))
							allowMessage = true
						}
					}
				}
			}
		}

		// blocklist for pubkeys overrides the ACLs above this
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

		// blocklist for keywords overrides the ACLs above this
		if relay.BlockList.ListKeywords != nil && len(relay.BlockList.ListKeywords) >= 1 {
			// relay has blacklist keywords, deny messages matching any of these keywords to post
			for _, k := range relay.BlockList.ListKeywords {
				dEvent := strings.ToLower(e.Event.Content)
				dKeyword := strings.ToLower(k.Keyword)
				if strings.Contains(dEvent, dKeyword) {
					log("rejecting for keyword: " + k.Keyword)
					badResp = "blocked. " + k.Keyword + " reason: " + k.Reason
					allowMessage = false
				}
			}
		}

		// NIP59, NIP87, NIP86 (private groups/giftwrap allow)
		if relay.AllowGiftwrap {
			if e.Event.Kind == 13 || e.Event.Kind == 1059 || e.Event.Kind == 1060 || e.Event.Kind == 24 || e.Event.Kind == 25 || e.Event.Kind == 26 || e.Event.Kind == 27 || e.Event.Kind == 35834 {
				// allow all gifts
				allowMessage = true
				log("allowing for gift, kind: " + fmt.Sprintf("%d", e.Event.Kind))
			}
		}

		// Kind checking
		// if a kind is blocked, it overrides all other ACLs above this
		if relay.BlockList.ListKinds != nil && len(relay.BlockList.ListKinds) >= 1 {
			for _, k := range relay.BlockList.ListKinds {
				log("checking kind: " + fmt.Sprintf("%d, %d", k.Kind, e.Event.Kind))
				if e.Event.Kind == k.Kind {
					log("rejecting for kind: " + fmt.Sprintf("%d", k.Kind))
					badResp = "blocked kind " + fmt.Sprintf("%d", k.Kind) + " reason: " + k.Reason
					allowMessage = false
				}
			}
		}

		if !allowMessage {
			result.Action = "reject"
			result.Msg = badResp
		}

		r, _ := json.Marshal(result)
		output.WriteString(fmt.Sprintf("%s\n", r))
		output.Flush()

		if influxEnabled {
			blocked := 0
			allowed := 1
			if !allowMessage {
				blocked = 1
				allowed = 0
			}

			p := influxdb2.NewPoint(
				iConfig.Measurement,
				map[string]string{
					"kind":  fmt.Sprintf("%d", e.Event.Kind),
					"relay": relay.ID,
				},
				map[string]interface{}{
					"event":   1,
					"blocked": blocked,
					"allowed": allowed,
				},
				time.Now())
			// write asynchronously
			writeAPI.WritePoint(p)
		}
	}
}
