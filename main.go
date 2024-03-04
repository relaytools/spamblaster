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
	ID                   string      `json:"id"`
	Name                 string      `json:"name"`
	OwnerID              string      `json:"ownerId"`
	DefaultMessagePolicy bool        `json:"default_message_policy"`
	AllowGiftwrap		bool        `json:"allow_giftwrap"`
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

	ticker := time.NewTicker(30 * time.Second)
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
		if e.Event.Kind == 1984 {
			if isModAction(relay, e) {
				log(fmt.Sprintf("1984 request from %s>", e.Event.Pubkey))
				// perform deletion of a single event
				// grab the event id

				thisReason := ""
				thisEvent := ""
				for _, x := range e.Event.Tags {
					if x[0] == "e" {
						thisEvent = x[1]
						if len(x) == 3 {
							thisReason = x[2]
						}
					}
				}

				if thisEvent != "" {
					log(fmt.Sprintf("received 1984 from mod: %s, delete event <%s>, reason: %s", e.Event.Pubkey, thisEvent, thisReason))
					// shell out
					filter := fmt.Sprintf("{\"ids\": [\"%s\"]}", thisEvent)
					cmd := exec.Command("/app/strfry", "delete", "--filter", filter)
					out, err := cmd.Output()
					if err != nil {
						log(fmt.Sprintln("could not run command: ", err))
					}
					log(fmt.Sprintln("strfry command output: ", string(out)))

					//cmd.Run()
				}

				// if modaction is for a pubkey, post back to the API for block_list pubkey
				// also delete the events related to this pubkey
				thisPubkey := ""
				for _, x := range e.Event.Tags {
					if x[0] == "p" {
						thisPubkey = x[1]
						if len(x) == 3 {
							thisReason = x[2]
						}
					}
				}

				// event should be blank if we're getting a report about just a pubkey
				if thisPubkey != "" && thisEvent == "" {
					log(fmt.Sprintf("received 1984 from mod: %s, block and delete pubkey <%s>, reason: %s", e.Event.Pubkey, thisPubkey, thisReason))
					// shell out
					filter := fmt.Sprintf("{\"authors\": [\"%s\"]}", thisPubkey)
					cmd := exec.Command("/app/strfry", "delete", "--filter", filter)
					out, err := cmd.Output()
					if err != nil {
						log(fmt.Sprintln("could not run command: ", err))
					}
					log(fmt.Sprintln("strfry command output: ", string(out)))
					//cmd.Run()
					// TODO: call to api
				}

			}
		}

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
						}
					} else {
						log("error decoding pubkey: " + k.Pubkey + " " + err.Error())
					}
				}

				if strings.Contains(e.Event.Pubkey, k.Pubkey) {
					log("allowing whitelist for pubkey: " + k.Pubkey)
					allowMessage = true
				}
			}
		}

		// keywords logic
		if relay.AllowList.ListKeywords != nil && len(relay.AllowList.ListKeywords) >= 1 && !relay.DefaultMessagePolicy {
			// relay has whitelist keywords, allow  messages matching any of these keywords to post, deny messages that don't.
			// todo: what about if they're allow_listed pubkey? (currently this would allow either)
			for _, k := range relay.AllowList.ListKeywords {
				dEvent := strings.ToLower(e.Event.Content)
				dKeyword := strings.ToLower(k.Keyword)
				if strings.Contains(dEvent, dKeyword) {
					log("allowing for keyword: " + k.Keyword)
					allowMessage = true
				}
			}
		// The one specific case you wouldn't want to allow owner+mods is in this AllowList keywords mode
		// Therefor, we will do the mod detector check here and allow all owners+mods
		} else {
			// allow owners + moderators
			if isModAction(relay, e) {
				allowMessage = true
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
