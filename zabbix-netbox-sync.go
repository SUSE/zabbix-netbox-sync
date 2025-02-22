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
	var netboxToken string
	var zabbixUser string
	var zabbixPassphrase string

	flag.StringVar(&logLevelStr, "loglevel", "info", "Logging level")
	flag.StringVar(&netboxUrl, "netbox", "", "URL to a NetBox instance")
	flag.StringVar(&zabbixUrl, "zabbix", "", "URL to a Zabbix instance")
	flag.Parse()

	logger = slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: convertLogLevel(logLevelStr)}))

	if zabbixUrl == "" || netboxUrl == "" {
		Error("Specify -netbox <URL> and -zabbix <URL>.")
		os.Exit(1)
	}

	netboxToken = os.Getenv("NETBOX_TOKEN")
	zabbixUser = os.Getenv("ZABBIX_USER")
	zabbixPassphrase = os.Getenv("ZABBIX_PASSPHRASE")

	if zabbixUser == "" {
		zabbixUser = "guest"
	}

	z := zConnect(zabbixUrl, zabbixUser, zabbixPassphrase)

	nb, nbctx := nbConnect(netboxUrl, netboxToken)

	getVirtualMachines(nb, nbctx)

	zabbixHosts := make(zabbixHosts)

	whitelistedHostgroups := []string{"Owners/Engineering/Infrastructure"}
	workHosts := getHosts(z, filterHostGroupIds(getHostGroups(z), whitelistedHostgroups))
	workHostInterfaceIds := filterHostInterfaceIds(filterHostInterfaces(&zabbixHosts, getHostInterfaces(z, filterHostIds(workHosts))))

	search := make(map[string][]string)
	search["_key"] = []string{
		"agent.hostname",
		"net.if.ip4[*]",
		"net.if.ip6[*]",
		"sys.hw.manufacturer",
		"sys.hw.metadata",
		"sys.mount.nfs",
		"sys.net.listen",
		"sys.os.release",
		"system.cpu.num",
		"system.sw.arch",
		"vm.memory.size[total]",
	}

	filterItems(&zabbixHosts, getItems(z, workHostInterfaceIds, search))

	for _, host := range zabbixHosts {
		Debug("Got host %s", host.HostName)

		if host.Error {
			Error("Skipping export of %s (%s), metrics contain errors.")
			continue
		}

		found := false
		for _, metric := range host.Metrics {
			Info(metric.Key)
			if metric.Key == "agent.hostname" {
				Info("Got %s => %s", metric.Key, metric.Value)
				found = true
				break
			}
		}

		if !found {
			Error("Skipping export of %s (%s), 'agent.hostname' is missing.", host.HostID, host.HostName)
			continue
		}
	}
}
