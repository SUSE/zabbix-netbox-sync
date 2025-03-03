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

func processVirtualMachine(host *zabbixHostData, nb *netbox.APIClient, ctx context.Context, dryRun bool) {
	name := host.HostName
	query, _, err := nb.VirtualizationAPI.VirtualizationVirtualMachinesList(ctx).Name([]string{name}).Limit(2).Execute()
	handleError("Query of virtual machines", err)
	found := query.Results
	Info("Found virtual machines: %+v", found)
	foundcount := len(found)

	memory := *netbox.NewNullableInt32(&host.Memory)
	vcpus := *netbox.NewNullableFloat64(&host.CPUs)

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
				Memory:  memory,
				Vcpus:   vcpus,
			}
			Debug("Payload: %+v", request)
			created, response, rerr := nb.VirtualizationAPI.VirtualizationVirtualMachinesCreate(ctx).WritableVirtualMachineWithConfigContextRequest(request).Execute()
			handleResponse(created, response, rerr)
		}

	case 1:
		object := found[0]

		request := *netbox.NewPatchedWritableVirtualMachineWithConfigContextRequest()

		memory_new := *memory.Get()
		memory_old := *object.Memory.Get()
		if memory_new != memory_old {
			Info("Memory changed: %d => %d", memory_old, memory_new)
			request.Memory = memory
		}

		vcpus_new := *vcpus.Get()
		vcpus_old := *object.Vcpus.Get()
		if vcpus_new != vcpus_old {
			Info("vCPUs changed: %f => %f", vcpus_old, vcpus_new)
			request.Vcpus = vcpus
		}

		if request.HasMemory() || request.HasVcpus() {
			Debug("Payload: %+v", request)

			if dryRun {
				Info("Would patch object")
				return
			}

			created, response, rerr := nb.VirtualizationAPI.VirtualizationVirtualMachinesPartialUpdate(ctx, object.Id).PatchedWritableVirtualMachineWithConfigContextRequest(request).Execute()
			handleResponse(created, response, rerr)

		} else {
			Info("Nothing to do")
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
