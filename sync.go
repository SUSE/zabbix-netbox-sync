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
	"github.com/fabiang/go-zabbix"
	"github.com/netbox-community/go-netbox/v4"
)

func prepare(z *zabbix.Session, zh *zabbixHosts) {
	whitelistedHostgroups := []string{"Owners/Engineering/Infrastructure"}
	workHosts := getHosts(z, filterHostGroupIds(getHostGroups(z), whitelistedHostgroups))
	hostIds := filterHostIds(workHosts)
	filterHostInterfaces(zh, getHostInterfaces(z, hostIds))

	search := make(map[string][]string)
	search["key_"] = []string{
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

	filterItems(zh, getItems(z, hostIds, search), search["key_"])
	scanHosts(zh)
}

func sync(zh *zabbixHosts, nb *netbox.APIClient, ctx context.Context) {
	for _, host := range *zh {
		if host.Error {
			Warn("Skipping processing of host %s.", host.HostName)

			continue
		}

		Info("Processing host %s", host.HostName)

		nbvms, _, err := nb.VirtualizationAPI.VirtualizationVirtualMachinesList(ctx).Name([]string{host.HostName}).Limit(2).Execute()
		handleError("Failed to query virtual machines: %s", err)
		vms := nbvms.Results
		Info("%v", vms)
		vmcount := len(vms)

		if vmcount == 0 {
			Info("Need to create VM")
		} else if vmcount == 1 {
			Info("Found VM")
		} else {
			Error("Host %s matches multiple virtual machine objects in NetBox.")
		}
	}
}
