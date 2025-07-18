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
	"sync"
	"sync/atomic"
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
		ListPubkeys []ListPubkey `json:"list_pubkeys"`
		ListKinds   []struct {
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

	AclSources []AclSource `json:"acl_sources"`
}

type ListPubkey struct {
	ID          string      `json:"id"`
	AllowListID string      `json:"AllowListId"`
	BlockListID interface{} `json:"BlockListId"`
	Pubkey      string      `json:"pubkey"`
	Reason      string      `json:"reason"`
	ExpiresAt   interface{} `json:"expires_at"`
}

type AclSource struct {
	ID      string `json:"id"`
	RelayID string `json:"relayId"`
	AclType string `json:"aclType"`
	Url     string `json:"url"`
}

type GrapevineACL struct {
	Success bool `json:"success"`
	Data    struct {
		Query      string   `json:"query"`
		NumPubkeys int      `json:"numPubkeys"`
		Pubkeys    []string `json:"pubkeys"`
	} `json:"data"`
	Kinds []int `json:"kinds,omitempty"`
}

type NIP05DomainACL struct {
	Names map[string]string `json:"names"`
}

var logfile *os.File
var errlog = bufio.NewWriter(os.Stderr)
var pubkeyMap sync.Map

// strfry was not passing through the logs, but now it seems to work.
// an intermittant logging problem that does not affect the rest of the operations
// logging to a file can be helpful in this case (disabled)
func initLogging() error {
	/*
		var err error
		logfile, err = os.OpenFile("spamblaster.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			errlog.WriteString(fmt.Sprintf("Error opening log file: %v\n", err))
			errlog.Flush()
			return err
		}
	*/
	return nil
}

func log(message string) {
	// Get current timestamp
	//timestamp := time.Now().Format("2006-01-02 15:04:05")
	//formattedMsg := fmt.Sprintf("[%s] %s", timestamp, message)

	// Write to stderr
	errlog.WriteString(message + "\n")
	errlog.Flush()

	// Write to log file if initialized
	//if logfile != nil {
	//	logfile.WriteString(formattedMsg + "\n")
	//}
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

func fetchGrapevine(aclSource AclSource, m *sync.Map) bool {
	// Set a timeout for the HTTP request
	client := &http.Client{
		Timeout: 20 * time.Second,
	}

	res, err := client.Get(aclSource.Url)
	if err != nil {
		log(fmt.Sprintf("Error fetching Grapevine ACL: %s", err.Error()))
		return false
	}
	defer res.Body.Close()
	log(fmt.Sprintf("HTTP GET successful with status code: %d", res.StatusCode))

	if res.StatusCode != 200 {
		log(fmt.Sprintf("Grapevine ACL status code error: %d", res.StatusCode))
		return false
	}

	body, readErr := io.ReadAll(res.Body)
	if readErr != nil {
		log(fmt.Sprintf("Error reading Grapevine ACL response: %s", readErr.Error()))
		return false
	}

	var grapevineAcl GrapevineACL

	jsonErr := json.Unmarshal(body, &grapevineAcl)
	if jsonErr != nil {
		log(fmt.Sprintf("Error unmarshaling Grapevine ACL: %s", jsonErr.Error()))
		return false
	}

	updateSyncMapFromGrapevine(grapevineAcl, m, aclSource.ID)

	log(fmt.Sprintf("Successfully processed Grapevine ACL with %d(%d) pubkeys", len(grapevineAcl.Data.Pubkeys), grapevineAcl.Data.NumPubkeys))
	return true
}

func fetchNip05(aclSource AclSource, m *sync.Map) bool {
	log(fmt.Sprintf("Fetching NIP05 Domain ACL from: %s", aclSource.Url))

	// Ensure the URL ends with /.well-known/nostr.json
	processedUrl := aclSource.Url
	if !strings.HasSuffix(processedUrl, "/.well-known/nostr.json") {
		if !strings.HasSuffix(processedUrl, "/") {
			processedUrl += "/"
		}
		processedUrl += ".well-known/nostr.json"
	}

	log(fmt.Sprintf("Attempting HTTP GET to: %s", processedUrl))

	// Set a timeout for the HTTP request
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	res, err := client.Get(processedUrl)
	if err != nil {
		log(fmt.Sprintf("Error fetching NIP05 Domain ACL: %s", err.Error()))
		return false
	}
	defer res.Body.Close()
	log(fmt.Sprintf("HTTP GET successful with status code: %d", res.StatusCode))

	if res.StatusCode != 200 {
		log(fmt.Sprintf("NIP05 Domain ACL status code error: %d", res.StatusCode))
		return false
	}

	body, readErr := io.ReadAll(res.Body)
	if readErr != nil {
		log(fmt.Sprintf("Error reading NIP05 Domain ACL response: %s", readErr.Error()))
		return false
	}

	var nip05DomainAcl NIP05DomainACL
	jsonErr := json.Unmarshal(body, &nip05DomainAcl)
	if jsonErr != nil {
		log(fmt.Sprintf("Error unmarshaling NIP05 Domain ACL: %s", jsonErr.Error()))
		return false
	}

	updateSyncMapFromNip05(nip05DomainAcl, m, aclSource.ID)

	log(fmt.Sprintf("Successfully processed NIP05 Domain ACL with %d pubkeys", len(nip05DomainAcl.Names)))
	return true
}

func updateSyncMapFromNip05(np NIP05DomainACL, m *sync.Map, source string) {
	for _, p := range np.Names {
		m.LoadOrStore(p, source)
	}
	cleanupSyncMapFromNip05(np, m, source)
}

func cleanupSyncMapFromNip05(np NIP05DomainACL, m *sync.Map, source string) {
	m.Range(func(k, v interface{}) bool {
		if v != source {
			return true
		}
		notfound := true
		for _, i := range np.Names {
			if i == k {
				notfound = false
			}
		}
		if notfound {
			log(fmt.Sprintf("removing entry for %s :%s", k, source))
			m.Delete(k)
		}
		return true
	})

}

func cleanupSyncMapFromGrapevine(gv GrapevineACL, m *sync.Map, source string) {
	m.Range(func(k, v interface{}) bool {
		if v != source {
			return true
		}
		notfound := true
		for _, i := range gv.Data.Pubkeys {
			if i == k {
				notfound = false
			}
		}
		if notfound {
			log(fmt.Sprintf("removing entry for %s :%s", k, source))
			m.Delete(k)
		}
		return true
	})

}

func updateSyncMapFromGrapevine(gv GrapevineACL, m *sync.Map, source string) {
	for _, p := range gv.Data.Pubkeys {
		m.LoadOrStore(p, source)
	}
	cleanupSyncMapFromGrapevine(gv, m, source)
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

func mapLen(m *sync.Map) int64 {
	var counter int64
	m.Range(func(k, v interface{}) bool {
		atomic.AddInt64(&counter, 1)
		return true
	})
	return counter
}

func updateSyncMapFromRelay(relay Relay, m *sync.Map) {
	for _, p := range relay.AllowList.ListPubkeys {
		// legacy, sometimes they're not in hex here
		usekey := p.Pubkey
		if strings.Contains(p.Pubkey, "npub") {
			if _, v, err := nip19.Decode(p.Pubkey); err == nil {
				usekey = v.(string)
			} else {
				log("error decoding pubkey: " + p.Pubkey + " " + err.Error())
			}
		}
		// store with the source mentioned here as relay
		m.LoadOrStore(usekey, "relay")

		// cleanup pubkeys that have been removed from ListPubkeys
	}

	cleanupSyncMapFromRelay(relay.AllowList.ListPubkeys, m)
	//log(fmt.Sprintf("mapLen size is: %d", mapLen(m)))
	log(fmt.Sprintf("lp size is: %d", len(relay.AllowList.ListPubkeys)))
	//doubleCheckAllKeysExist(relay.AllowList.ListPubkeys, m)
}

func cleanupSyncMapFromRelay(lp []ListPubkey, m *sync.Map) {
	m.Range(func(k, v interface{}) bool {
		if v != "relay" {
			return true
		}
		notfound := true
		for _, i := range lp {
			usekey := i.Pubkey
			if strings.Contains(i.Pubkey, "npub") {
				if _, v, err := nip19.Decode(i.Pubkey); err == nil {
					usekey = v.(string)
				} else {
					log("error decoding pubkey: " + i.Pubkey + " " + err.Error())
				}
			}
			if usekey == k {
				notfound = false
			}
		}
		if notfound {
			m.Delete(k)
			log(fmt.Sprintf("removing entry for %s", k))
		}

		return true
	})
}

func doubleCheckAllKeysExist(lp []ListPubkey, m *sync.Map) {

	for _, i := range lp {
		usekey := i.Pubkey
		if strings.Contains(i.Pubkey, "npub") {
			if _, v, err := nip19.Decode(i.Pubkey); err == nil {
				usekey = v.(string)
			} else {
				log("error decoding pubkey: " + i.Pubkey + " " + err.Error())
			}
		}
		_, ok := m.Load(usekey)
		if !ok {
			log(fmt.Sprintf("ERROR: key was not found in map: %s", usekey))
		}
	}
}

func main() {
	var reader = bufio.NewReader(os.Stdin)
	var output = bufio.NewWriter(os.Stdout)
	defer output.Flush()
	defer errlog.Flush()

	// Initialize logging to both stderr and file
	if err := initLogging(); err != nil {
		log("Warning: Could not initialize log file, continuing with stderr logging only")
	} else {
		defer logfile.Close()
		log("Logging initialized successfully to both stderr and spamblaster.log")
	}

	var err1 error
	var relay Relay

	relay, err1 = queryRelay(relay)
	if err1 != nil {
		log("there was an error fetching relay, using cache or nil: " + err1.Error())
	} else {
		updateSyncMapFromRelay(relay, &pubkeyMap)
	}

	aclListener := make(chan []AclSource)

	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	go func() {
		for {
			<-ticker.C
			relay, err1 = queryRelay(relay)
			if err1 != nil {
				log("there was an error fetching relay, using cache or nil" + err1.Error())
			} else {
				updateSyncMapFromRelay(relay, &pubkeyMap)
			}
			aclListener <- relay.AclSources
		}
	}()

	go func() {
		var oldAclSources []AclSource
		var allTimers = make(map[string]*time.Ticker)

		for {
			a := <-aclListener
			if len(oldAclSources) != len(a) {
				log("aclListeners updating")
				for _, as := range a {
					foundOld := false
					for _, o := range oldAclSources {
						if o.ID == as.ID {
							// already done
							log(fmt.Sprintf("already done %s", as.Url))
							foundOld = true
							continue
						}
					}
					if !foundOld {
						// setup new acl (initial fetch)
						log(fmt.Sprintf("setting up new %s:%s", as.Url, as.ID))
						if as.AclType == "grapevine" {
							fetchGrapevine(as, &pubkeyMap)
						} else if as.AclType == "nip05" {
							fetchNip05(as, &pubkeyMap)
						} else {
							log("unknown type" + as.AclType)
						}

						// stagger the fetch
						time.Sleep(time.Second * 30)

						// setup new acl (ticker)
						newTimer := time.NewTicker(60 * time.Minute)
						allTimers[as.ID] = newTimer

						go func(thisAcl AclSource) {
							for {
								<-newTimer.C

								// here we kick off a new acl listener
								if thisAcl.AclType == "grapevine" {
									fetchGrapevine(thisAcl, &pubkeyMap)
								} else if thisAcl.AclType == "nip05" {
									fetchNip05(thisAcl, &pubkeyMap)
								} else {
									log("unknown type" + thisAcl.AclType)
								}
							}
						}(as)
					}
				}

				// cleanup deleted
				for _, o := range oldAclSources {
					foundNew := false
					for _, aa := range a {
						if aa.ID == o.ID {
							foundNew = true
						}
					}
					if !foundNew {
						// cleanup
						log(fmt.Sprintf("cleaning up %s ", o.Url))
						allTimers[o.ID].Stop()
						// TODO cleanup the pubkeyMap
						counter := 0
						pubkeyMap.Range(func(key, value any) bool {
							if value == o.ID {
								counter += 1
								pubkeyMap.Delete(key)
							}
							return true
						})
						log(fmt.Sprintf("deleted %d pubkeys from map source removal", counter))
					}
				}
				oldAclSources = a
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
			if value, ok := pubkeyMap.Load(e.Event.Pubkey); value != "" && ok {
				// allowed
				log(fmt.Sprintf("allowing whitelist for %s from source:%s", e.Event.Pubkey, value))
				allowMessage = true
			}

			// if we're allowing tags, check if pubkey is tagged in the messages ptags
			if relay.AllowTagged {
				if e.Event.Tags != nil && len(e.Event.Tags) >= 1 {
					for _, x := range e.Event.Tags {
						if x[0] == "p" {
							if value, ok := pubkeyMap.Load(x[1]); value != "" && ok {
								log(fmt.Sprintf("allowing whitelist for tagged pubkey: %s, %s ", x[1], value))
								allowMessage = true
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
				if e.Event.Kind == k.Kind {
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
