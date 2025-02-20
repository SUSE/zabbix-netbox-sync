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
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/netbox-community/go-netbox/v4"

	// github.com/zabbix-tools/go-zabbix is not compatible with v6
	"github.com/fabiang/go-zabbix"
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

	zabbixUrl = fmt.Sprintf("%s/api_jsonrpc.php", zabbixUrl)

	Debug("Connecting to Zabbix at %s", zabbixUrl)
	z, err := zabbix.NewSession(zabbixUrl, zabbixUser, zabbixPassphrase)
	handleError("Connection to Zabbix", err)

	Debug("Connecting to NetBox at %s", netboxUrl)
	nbctx := context.Background()
	nb := netbox.NewAPIClientFor(netboxUrl, netboxToken)

	nbres, _, err := nb.VirtualizationAPI.VirtualizationVirtualMachinesList(nbctx).Status([]string{"active"}).Limit(10).Execute()
	handleError("Querying virtual machines", err)

	Debug("%v", nbres.Results)

	whitelistedHostgroups := []string{"Owners/Engineering/Infrastructure"}
	hostGroupParams := zabbix.HostgroupGetParams{}
	allHostGroups, err := z.GetHostgroups(hostGroupParams)
	handleError("Querying host groups", err)
	Debug("All host groups: %v", allHostGroups)

	workHostGroupIds := make([]string, 0, len(whitelistedHostgroups))
	for _, ahg := range allHostGroups {
		if contains(whitelistedHostgroups, ahg.Name) {
			workHostGroupIds = append(workHostGroupIds, ahg.GroupID)
		}
	}
	Debug("Filtered host group IDs: %v", workHostGroupIds)

	hostParams := zabbix.HostGetParams{
		GroupIDs: workHostGroupIds,
	}
	workHosts, err := z.GetHosts(hostParams)
	handleError("Querying hosts", err)

	workHostIds := make([]string, 0, len(workHosts))
	for _, wh := range workHosts {
		workHostIds = append(workHostIds, wh.HostID)
	}
	Debug("Filtered host IDs: %v", workHostIds)

	interfaceParams := zabbix.HostInterfaceGetParams{
		HostIDs: workHostIds,
	}
	hostInterfaces, err := z.GetHostInterfaces(interfaceParams)
	handleError("Querying host interfaces", err)

	/*
	var workHostInterfaces []zabbix.HostInterface
	for _, whi := range hostInterfaces {
		if whi.Type == 1 {
			workHostInterfaces = append(workHostInterfaces, whi)
		}
	}
	Debug("Filtered host interfaces: %v", workHostInterfaces)
	*/

	var workHostInterfaceIds []string
	for _, whi := range hostInterfaces {
		workHostInterfaceIds = append(workHostInterfaceIds, whi.InterfaceID)
	}
	Debug("Filtered host interface IDs: %v", workHostInterfaceIds)

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

	itemParams := zabbix.ItemGetParams{
		//GetParameters: searchGetParameters,
		GetParameters: zabbix.GetParameters{
			SearchByAny:               true,
			EnableTextSearchWildcards: true,
			TextSearch:                search,
		},
		InterfaceIDs: workHostInterfaceIds,
	}
	items, err := z.GetItems(itemParams)
	handleError("Querying items", err)
	Debug("Items: %v", items)

	for _, item := range items {
		if item.Error != "" {
			Info("Host %s Item %s %s LastValue %s LastValueType %d Error %s", item.HostID, item.ItemID, item.ItemName, item.LastValue, item.LastValueType, item.Error)
		}
	}
}
