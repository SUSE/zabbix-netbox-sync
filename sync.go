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

func prepare(z *zabbix.Session, zh *zabbixHosts, whitelistedHostgroups []string) {
	workHosts := getHosts(z, filterHostGroupIds(getHostGroups(z), whitelistedHostgroups))
	hostIds := filterHostIds(workHosts)
	filterHostInterfaces(zh, getHostInterfaces(z, hostIds))

	search := make(map[string][]string)
	search["key_"] = []string{
		"agent.hostname",
		"net.if.ip.a.raw",
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

func processMacAddress(nb *netbox.APIClient, ctx context.Context, address string, dryRun bool) (int32, bool) {
	Debug("Processing MAC address %s", address)
	query, _, err := nb.DcimAPI.DcimMacAddressesList(ctx).MacAddress([]string{address}).Execute()
	handleError("Query of MAC addresses", err)
	found := query.Results

	var objid int32
	var assigned bool

	switch len(found) {
	case 0:
		if dryRun {
			Info("Would create MAC address object '%s'", address)
			return objid, assigned
		}

		Info("Creating MAC address object '%s'", address)

		created, response, rerr := nb.DcimAPI.DcimMacAddressesCreate(ctx).MACAddressRequest(*netbox.NewMACAddressRequest(address)).Execute()
		handleResponse(created, response, rerr)

		objid = created.Id
		assigned = false

	case 1:
		Debug("MAC address object '%s' already exists", address)

		objid = found[0].Id

		if found[0].AssignedObjectType.IsSet() && found[0].AssignedObjectId.IsSet() {
			assigned = true
		}

	default:
		Warn("MAC address object '%s' exists multiple times", address)
	}

	return objid, assigned
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

	var vmobjid int32

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
			vmobjid = created.Id
		}

	case 1:
		object := found[0]

		request := *netbox.NewPatchedWritableVirtualMachineWithConfigContextRequest()

		memory_new := *memory.Get()
		var memory_old int32
		if object.Memory.Get() != nil {
			memory_old = *object.Memory.Get()
		}
		if memory_new != memory_old {
			Info("Memory changed: %d => %d", memory_old, memory_new)
			request.Memory = memory
		}

		vcpus_new := *vcpus.Get()
		var vcpus_old float64
		if object.Vcpus.Get() != nil {
			vcpus_old = *object.Vcpus.Get()
		}
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
			vmobjid = created.Id

		} else {
			Info("Nothing to do")
			vmobjid = object.Id
		}

	default:
		Error("Host %s matches multiple (%d) objects in NetBox.", name, foundcount)
	}

	var iffound []netbox.VMInterface

	if vmobjid > 0 {
		ifquery, response, err := nb.VirtualizationAPI.VirtualizationInterfacesList(ctx).VirtualMachineId([]int32{vmobjid}).Execute()
		handleResponse(ifquery, response, err)
		handleError("Query of virtual machine interfaces", err)
		iffound = ifquery.Results
		Info("Found virtual machine interfaces: %+v", iffound)
	}

	for _, inf := range host.Interfaces {
		if inf.IfName == "lo" {
			continue
		}

		mtu := *netbox.NewNullableInt32(&inf.Mtu)

		var found bool
		var intobjid int32
		var nbinf netbox.VMInterface

		Debug("Scanning %+v", inf)
		for _, nbif := range iffound {
			if inf.IfName == nbif.Name {
				// UPDATE
				found = true
				intobjid = nbif.Id
				nbinf = nbif

				break
			}
		}

		macobjid, macassigned := processMacAddress(nb, ctx, inf.Address, dryRun)

		if found {
			request := *netbox.NewPatchedWritableVMInterfaceRequest()

			mtu_new := *mtu.Get()
			mtu_old := *nbinf.Mtu.Get()
			if mtu_new != mtu_old {
				Info("MTU changed: %d => %d", mtu_old, mtu_new)
				request.Mtu = mtu
			}

			// TODO: compare/update tagged VLANs

			if request.HasMtu() {
				Debug("Payload: %+v", request)

				if dryRun {
					Info("Would patch object")
					continue
				}

				created, response, rerr := nb.VirtualizationAPI.VirtualizationInterfacesPartialUpdate(ctx, intobjid).PatchedWritableVMInterfaceRequest(request).Execute()
				handleResponse(created, response, rerr)
			}
		} else if !found {
			if dryRun {
				Info("Would create interface object")
			} else {
				request := netbox.WritableVMInterfaceRequest{
					VirtualMachine: *netbox.NewBriefVirtualMachineRequest(name),
					Name:           inf.IfName,
					Mtu:            mtu,
					TaggedVlans:    *new([]int32),
					Enabled:        netbox.PtrBool(true),
				}

				mode, err := netbox.NewPatchedWritableInterfaceRequestModeFromValue("tagged")
				handleError("Constructing 802.1Q mode from string", err)

				if inf.LinkInfo.Kind == "vlan" {
					request.Mode = *netbox.NewNullablePatchedWritableInterfaceRequestMode(mode)
					request.TaggedVlans = append(request.TaggedVlans, inf.LinkInfo.Data.(iproute2LinkInfoDataVlan).Id)
				}

				created, response, rerr := nb.VirtualizationAPI.VirtualizationInterfacesCreate(ctx).WritableVMInterfaceRequest(request).Execute()
				handleResponse(created, response, rerr)

				intobjid = created.Id

			}
		}

		if macobjid > 0 && !macassigned && !dryRun {
			assignMacAddress(nb, ctx, macobjid, inf.Address, "virtualization.vminterface", int64(intobjid))
		}

	}
}

func sync(zh *zabbixHosts, nb *netbox.APIClient, ctx context.Context, dryRun bool, limit string) {
	for _, host := range *zh {
		if host.Error {
			Warn("Skipping processing of host %s.", host.HostName)

			continue
		}

		name := host.HostName

		if limit != "" && name != limit {
			continue
		}

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
