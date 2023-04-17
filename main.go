package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/lithammer/fuzzysearch/fuzzy"
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
	if _, err := logfile.WriteString(message); err != nil {
		log("error writing to file: " + err.Error())
	}
}

func log(message string) {
	errlog.WriteString(fmt.Sprintln(message))
	errlog.Flush()
}

var niceBots = []string{
	"8ebf24a6d1a0bb69f7bce5863fec286ff3a0ebafdff0e89e2ed219d83f219238", // cowdle
}

var niceContent = []string{
	"@cowdle #game",
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

func main() {
	initLogging()
	defer logfile.Close()

	var reader = bufio.NewReader(os.Stdin)
	var output = bufio.NewWriter(os.Stdout)
	defer output.Flush()
	defer errlog.Flush()

	var seen = make(map[string]time.Time)

	for {

		seen = expireSeen(seen)
		var input, _ = reader.ReadString('\n')
		log(fmt.Sprintf("invoked> spamblaster 0.0.1 -> seen cache size: %d", len(seen)))

		var e StrfryEvent
		if err := json.Unmarshal([]byte(input), &e); err != nil {
			panic(err)
		}

		result := StrfryResult{
			ID:     e.Event.ID,
			Action: "accept",
		}

		// whitelist addresses and content
		isNiceBot := false
		for _, b := range niceBots {
			if b == e.Event.Pubkey {
				isNiceBot = true
			}
		}
		for _, b := range niceContent {
			if b == e.Event.Content {
				isNiceBot = true
			}
		}

		// message
		if e.Event.Kind == 1 {
			badMessage := false
			seenDist := 1.00
			for i, _ := range seen {
				dist, tooSimilar := compareSimilar(i, e.Event.Content)
				if tooSimilar {
					badMessage = true
					seenDist = dist
				}
			}
			/*
				if badMessage {
					// url shorteners for meme lords
					if len(e.Event.Content) == 32 && dist  {
						if strings.HasPrefix(e.Event.Content, "https://nostr.build/") || strings.HasPrefix(e.Event.Content, "https://imgflip.com/") || strings.HasPrefix(e.Event.Content, "https://imgur.com/") {
							badMessage = false
						}
					}
				}
			*/
			if badMessage {
				result.Action = "reject"
				result.Msg = "blocked by spamblaster. reason: duplicate message"
				logFile(fmt.Sprintf("blocked,%.2f,%s,%s,%s,%s\n", seenDist, e.SourceInfo, e.Event.Pubkey, e.Event.Content, time.Now()))
			} else {
				logFile(fmt.Sprintf("message,%s,%s,%s\n", e.SourceInfo, e.Event.Pubkey, e.Event.Content))

				if len(e.Event.Content) > 20 && !isNiceBot {
					seen[e.Event.Content] = time.Now()
				}
			}
		}

		// channel message
		if e.Event.Kind == 42 {
			badMessage := false
			seenDist := 1.00
			for i, _ := range seen {
				dist, tooSimilar := compareSimilar(i, e.Event.Content)
				if tooSimilar {
					badMessage = true
					seenDist = dist
				}
			}
			/*
				if badMessage {
					// url shorteners for meme lords
					if len(e.Event.Content) == 32 {
						if strings.HasPrefix(e.Event.Content, "https://nostr.build/") || strings.HasPrefix(e.Event.Content, "https://imgflip.com/") || strings.HasPrefix(e.Event.Content, "https://imgur.com/") {
							badMessage = false
						}
					}
				}
			*/
			if badMessage {
				result.Action = "reject"
				result.Msg = "blocked by spamblaster. reason: duplicate message"
				logFile(fmt.Sprintf("blocked,%.2f,%s,%s,%s,%s\n", seenDist, e.SourceInfo, e.Event.Pubkey, e.Event.Content, time.Now()))
			} else {
				logFile(fmt.Sprintf("cmessage,%s,%s,%s\n", e.SourceInfo, e.Event.Pubkey, e.Event.Content))
				if len(e.Event.Content) > 20 && !isNiceBot {
					seen[e.Event.Content] = time.Now()
				}
			}
		}

		r, _ := json.Marshal(result)
		output.WriteString(fmt.Sprintf("%s\n", r))
		output.Flush()
	}

}
