// Copyright (C) 2022-2026, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"log"

	"github.com/TravelTokenMarketplace/travel-token-matrix-app-service/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		log.Fatalln(err)
	}
}
