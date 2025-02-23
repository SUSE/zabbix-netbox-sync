/*
   Zabbix -> NetBox synchronization tool
   Copyright (C) 2025  SUSE LLC <georg.pfuetzenreuter@suse.com>

   This program is free software: you can redistribute it and/or modify
   it under the terms of the GNU General Public License as published by
   the Free Software Foundation, either version 3 of the License, or
   (at your option) any later version.

   This program is distributed in the hope that it will be useful,
   but WITHOUT ANY WARRANTY; without even the implied warranty of
   MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
   GNU General Public License for more details.

   You should have received a copy of the GNU General Public License
   along with this program.  If not, see <https://www.gnu.org/licenses/>.
*/

package main

import (
	"flag"
	"log/slog"
	"os"
)

var (
	logger *slog.Logger
)

func main() {
	var logLevelStr string
	var netboxUrl string
	var zabbixUrl string
	var runDry bool
	var runWet bool

	flag.StringVar(&logLevelStr, "loglevel", "info", "Logging level")
	flag.StringVar(&netboxUrl, "netbox", "", "URL to a NetBox instance")
	flag.StringVar(&zabbixUrl, "zabbix", "", "URL to a Zabbix instance")
	flag.BoolVar(&runDry, "dry", false, "Run without performing any changes")
	flag.BoolVar(&runWet, "wet", false, "Run and perform changes")
	flag.Parse()

	logger = slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: convertLogLevel(logLevelStr)}))

	problem := false

	if zabbixUrl == "" || netboxUrl == "" {
		Error("Specify -netbox <URL> and -zabbix <URL>.")
		problem = true
	}

	if runDry && runWet {
		Error("Specify -dry OR -wet, not both.")
		problem = true
	}

	if !runDry && !runWet {
		Error("Specify -dry OR -wet.")
		problem = true
	}

	if problem {
		os.Exit(1)
	}

	var netboxToken string
	var zabbixUser string
	var zabbixPassphrase string

	netboxToken = os.Getenv("NETBOX_TOKEN")
	zabbixUser = os.Getenv("ZABBIX_USER")
	zabbixPassphrase = os.Getenv("ZABBIX_PASSPHRASE")

	if zabbixUser == "" {
		zabbixUser = "guest"
	}

	z := zConnect(zabbixUrl, zabbixUser, zabbixPassphrase)
	nb, nbctx := nbConnect(netboxUrl, netboxToken)

	zh := make(zabbixHosts)
	prepare(z, &zh)
	sync(&zh, nb, nbctx, runDry)
}
