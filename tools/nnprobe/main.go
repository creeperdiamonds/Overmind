// Command nnprobe checks a Bedrock server's NetherNet listener step by step, the way a 26.60
// client reaches it: the HTTP status ping (GET /v1/join), then the WebRTC join (POST
// /v1/join/{id} carrying the SDP offer), then the Bedrock login over the data channels. It
// logs in without Xbox, so a server with login on stops it at the login step, which still
// proves the signalling and the transport.
//
//	go run ./tools/nnprobe -addr 127.0.0.1:19132
package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/df-mc/go-nethernet"
	"github.com/df-mc/go-nethernet/endpoint"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:19132", "host:port of the server's NetherNet signalling (TCP)")
	name := flag.String("name", "NNProbe", "name to log in with")
	spawn := flag.Bool("spawn", true, "after login, wait for the spawn too")
	verbose := flag.Bool("v", false, "log the NetherNet library's debug output")
	dump := flag.Bool("dump", false, "print the signalling requests and answers, SDP included")
	trace := flag.Bool("trace", false, "print the ID of every Bedrock packet sent and received")
	selfSigned := flag.Bool("selfsigned", false, "send a self-signed client identity (a=identity) instead of none")
	flag.Parse()

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	url := "http://" + *addr
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Step 1: the status ping, what the server list shows.
	hc := http.DefaultClient
	if *dump {
		hc = &http.Client{Transport: dumper{http.DefaultTransport}}
	}
	client := endpoint.ClientConfig{Logger: log, HTTPClient: hc}.New()
	if err := ping(ctx, url); err != nil {
		fail("status (GET /v1/join)", err)
	}

	// Steps 2 and 3: signalling, WebRTC transport, then the Bedrock login on top.
	start := time.Now()
	nd := nethernet.Dialer{Log: log}
	if *selfSigned {
		// A client identity the dialer signs itself, in the shape of the Xbox GameServerToken
		// a real client sends. A server that checks the token's issuer turns it away.
		key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
		if err != nil {
			fail("generate key", err)
		}
		if nd.Identity, err = nethernet.SelfSignedIdentity(key, "https://authorization.franchise.minecraft-services.net/"); err != nil {
			fail("self-signed identity", err)
		}
	}
	d := minecraft.Dialer{IdentityData: login.IdentityData{DisplayName: *name}, ErrorLog: log}
	if *trace {
		d.PacketFunc = func(h packet.Header, payload []byte, src, dst net.Addr) {
			dir := "->" // to the server
			if strings.HasPrefix(src.String(), "http") {
				dir = "<-" // the server's address is its signalling URL
			}
			fmt.Printf("  %s packet 0x%02x, %d bytes", dir, h.PacketID, len(payload))
			if h.PacketID == packet.IDDisconnect {
				var pk packet.Disconnect
				pk.Marshal(protocol.NewReader(bytes.NewReader(payload), 0, false))
				fmt.Printf(": reason %d, message %q", pk.Reason, pk.Message)
			}
			fmt.Println()
		}
	}
	conn, err := d.DialContextNetwork(ctx, minecraft.NetherNet{Signaling: client, Dialer: nd, Log: log}, url)
	if err != nil {
		fail("join", err)
	}
	defer conn.Close()
	fmt.Printf("OK joined over NetherNet in %v: local %v, remote %v\n", time.Since(start).Round(time.Millisecond), conn.LocalAddr(), conn.RemoteAddr())
	if !*spawn {
		return
	}
	if err := conn.DoSpawnContext(ctx); err != nil {
		fail("spawn", err)
	}
	gd := conn.GameData()
	fmt.Printf("OK spawned in %q at %v\n", gd.WorldName, gd.PlayerPosition)
	_ = conn.WritePacket(&packet.Text{TextType: packet.TextTypeChat, SourceName: *name, Message: "nnprobe was here"})
	time.Sleep(500 * time.Millisecond)
}

// ping sends the status request and prints the answer. BDS 1.26.60 answers 200 with an empty
// body; any 2xx tells the client the server speaks NetherNet, and the body, when there is one,
// is the server list entry.
func ping(ctx context.Context, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url+"/v1/join", nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "libhttpclient/1.0.0.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%s: %s", resp.Status, body)
	}
	if len(body) == 0 {
		fmt.Printf("OK status: %s, empty body\n", resp.Status)
		return nil
	}
	var status endpoint.Status
	if err := json.Unmarshal(body, &status); err != nil {
		return fmt.Errorf("decode %q: %w", body, err)
	}
	fmt.Printf("OK status: %s %s\n", resp.Status, body)
	return nil
}

// dumper prints each signalling request and its answer.
type dumper struct{ next http.RoundTripper }

func (d dumper) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		body, _ = io.ReadAll(req.Body)
		req.Body = io.NopCloser(bytes.NewReader(body))
	}
	fmt.Printf("=== %s %s\n%s\n", req.Method, req.URL, body)
	resp, err := d.next.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	answer, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(answer))
	fmt.Printf("=== %s %v\n%s\n", resp.Status, resp.Header, answer)
	return resp, nil
}

func fail(step string, err error) {
	fmt.Printf("FAIL %s: %v\n", step, err)
	os.Exit(1)
}
