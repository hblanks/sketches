// Simple UDP receiver for packets from the shelly plug.
// Painfully rendered out from Gemini. Oh well.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/netip"
	"os"
	"time"
)

// Raw JSON payload sent by a Shelly Plus Plug US.
//
// Switch payload docs: https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Switch#status
type NotifyStatus struct {
	Src    string `json:"src"`
	Dst    string `json:"dst"`
	Method string `json:"method"`
	Params struct {
		TS      float64 `json:"ts"`
		Switch0 struct {
			AEnergy struct {
				ByMinute []float64 `json:"by_minute"`
				MinuteTS int64     `json:"minute_ts"`
				Total    float64   `json:"total"`
			} `json:"aenergy"`
			APower  float64 `json:"apower"`
			Current float64 `json:"current"`
			Voltage float64 `json:"voltage"`
		} `json:"switch:0"`
	} `json:"params"`
}

type Output struct {
	Timestamp float64 `json:"timestamp"`
	Total     float64 `json:"total"`
	APower    float64 `json:"apower"`
	Current   float64 `json:"current"`
	Voltage   float64 `json:"voltage"`
}

func processPacket(data []byte, output *Output) error {
	var payload NotifyStatus
	if err := json.Unmarshal(data, &payload); err != nil {
		return fmt.Errorf("error unmarshalling JSON: %w", err)
	}
	if payload.Method != "NotifyStatus" {
		return fmt.Errorf("unknown method: %s", payload.Method)
	}
	*output = Output{
		Timestamp: payload.Params.TS,
		APower:    payload.Params.Switch0.APower,
		Current:   payload.Params.Switch0.Current,
		Voltage:   payload.Params.Switch0.Voltage,
		Total:     payload.Params.Switch0.AEnergy.Total,
	}
	return nil
}

func main() {
	addrStr := os.Getenv("LISTEN_ADDR")
	if addrStr == "" {
		addrStr = "127.0.0.1:7777"
	}

	addrPort, err := netip.ParseAddrPort(addrStr)
	if err != nil {
		log.Fatalf("Invalid value for LISTEN_ADDR: %s", err)
	}

	udpAddr := net.UDPAddrFromAddrPort(addrPort)
	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		log.Fatalf("Error listening on UDP address %s: %v", addrStr, err)
	}
	defer conn.Close()
	log.Printf("Listening on %s", addrStr)

	buffer := make([]byte, 65535)
	for {
		n, remoteAddr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			log.Printf("Error reading from UDP: %v", err)
			time.Sleep(1 * time.Second)
			continue
		}

		var output Output
		err = processPacket(buffer[:n], &output)
		if err != nil {
			log.Printf("Error processing packet from %s: %v",
				remoteAddr.String(), err)
			continue
		}

		outputJSON, err := json.Marshal(output)
		if err != nil {
			log.Printf("Error marshalling output JSON: %v", err)
			continue
		}

		fmt.Printf("%s\n", outputJSON)
	}
}
