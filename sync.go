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
	"encoding/json"
	"github.com/fabiang/go-zabbix"
	"github.com/netbox-community/go-netbox/v4"
	"os"
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

func processVirtualMachine(host *zabbixHostData, nb *netbox.APIClient, ctx context.Context, dryRun bool) {
	name := host.HostName
	query, _, err := nb.VirtualizationAPI.VirtualizationVirtualMachinesList(ctx).Name([]string{name}).Limit(2).Execute()
	handleError("Query of virtual machines", err)
	found := query.Results
	Info("Found virtual machines: %v", found)
	foundcount := len(found)
	switch foundcount {
	case 0:
		if dryRun {
			Info("Would create virtual machine object")
		} else {
			Info("Creating virtual machine object")

			status, err := netbox.NewInventoryItemStatusValueFromValue("active")
			if err != nil {
				handleError("Validation of new status value", err)
			}

			request := netbox.WritableVirtualMachineWithConfigContextRequest{
				Name:    name,
				Site:    *netbox.NewNullableBriefSiteRequest(netbox.NewBriefSiteRequest("Prague - PRG2", "prg2")),
				Cluster: *netbox.NewNullableBriefClusterRequest(netbox.NewBriefClusterRequest("Unmapped")),
				Status:  status,
				Memory:  *netbox.NewNullableInt32(&host.Memory),
				Vcpus:   *netbox.NewNullableFloat64(&host.CPUs),
			}
			Debug("Payload: %+v", request)
			created, response, rerr := nb.VirtualizationAPI.VirtualizationVirtualMachinesCreate(ctx).WritableVirtualMachineWithConfigContextRequest(request).Execute()
			if rerr != nil {
				Error("Creation of new virtual machine object failed, API returned: %s", rerr)
			}
			var body interface{}
			jerr := json.NewDecoder(response.Body).Decode(&body)
			handleError("Decoding response body", jerr)
			if body != nil {
				if rerr == nil {
					Debug("%+v", body)
				} else {
					Error("%+v", body)
				}
			}
			if rerr != nil || jerr != nil {
				os.Exit(1)
			}
			Debug("Created %+v", created)
		}

	case 1:
		if dryRun {
			Info("Would compare virtual machine object")
		} else {
			Info("TODO")
		}

	default:
		Error("Host %s matches multiple (%d) objects in NetBox.", name, foundcount)
	}
}


func sync(zh *zabbixHosts, nb *netbox.APIClient, ctx context.Context, dryRun bool) {
	for _, host := range *zh {
		if host.Error {
			Warn("Skipping processing of host %s.", host.HostName)

			continue
		}

		name := host.HostName

		Info("Processing host %s", name)

		nbname := []string{name}

		switch host.ObjType {

		case "Virtual":
			processVirtualMachine(host, nb, ctx, dryRun)

		case "Physical":
			query, _, err := nb.DcimAPI.DcimDevicesList(ctx).Name(nbname).Limit(2).Execute()
			handleError("Query of devices", err)
			found := query.Results
			Info("Found devices: %v", found)
		}
	}
}
