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
	"fmt"
	"github.com/fabiang/go-zabbix"
	"gopkg.in/yaml.v3"
)

type zabbixMetric struct {
	ID    string
	Key   string
	Name  string
	Value string
	Error string
}

type zabbixHostMetaData map[string]string

type zabbixHostData struct {
	HostID   string
	HostName string
	Metrics  []zabbixMetric
	Error    bool
	ObjType  string
	Meta     zabbixHostMetaData
	Label    string
}

type zabbixHosts map[string]*zabbixHostData

func zConnect(baseUrl string, user string, pass string) *zabbix.Session {
	url := fmt.Sprintf("%s/api_jsonrpc.php", baseUrl)

	Debug("Connecting to Zabbix at %s", url)

	z, err := zabbix.NewSession(url, user, pass)
	handleError("Connection to Zabbix", err)

	return z
}

func getHostGroups(z *zabbix.Session) []zabbix.Hostgroup {
	hostGroups, err := z.GetHostgroups(zabbix.HostgroupGetParams{})
	handleError("Querying host groups", err)

	Debug("All host groups: %v", hostGroups)

	return hostGroups
}

func filterHostGroupIds(hostGroups []zabbix.Hostgroup, whitelist []string) []string {
	hostGroupIds := make([]string, 0, len(whitelist))
	for _, hg := range hostGroups {
		if contains(whitelist, hg.Name) {
			hostGroupIds = append(hostGroupIds, hg.GroupID)
		}
	}

	Debug("Filtered host group IDs: %v", hostGroupIds)

	return hostGroupIds
}

func getHosts(z *zabbix.Session, groupIds []string) []zabbix.Host {
	workHosts, err := z.GetHosts(zabbix.HostGetParams{
		GroupIDs: groupIds,
	})
	handleError("Querying hosts", err)

	return workHosts
}

func filterHostIds(hosts []zabbix.Host) []string {
	hostIds := make([]string, 0, len(hosts))
	for _, h := range hosts {
		hostIds = append(hostIds, h.HostID)
	}

	Debug("Filtered host IDs: %v", hostIds)

	return hostIds
}

func getHostInterfaces(z *zabbix.Session, hostIds []string) []zabbix.HostInterface {
	hostInterfaces, err := z.GetHostInterfaces(zabbix.HostInterfaceGetParams{
		HostIDs: hostIds,
	})
	handleError("Querying host interfaces", err)

	return hostInterfaces
}

func filterHostInterfaces(zh *zabbixHosts, interfaces []zabbix.HostInterface) []zabbix.HostInterface {
	var hostInterfaces []zabbix.HostInterface
	for _, iface := range interfaces {
		if iface.Type == 1 {
			hostInterfaces = append(hostInterfaces, iface)
			hostname := iface.DNS
			if hostname == "" {
				Debug("Empty DNS field in interface %s", iface.InterfaceID)
				hostname = iface.IP
			}
			(*zh)[iface.HostID] = &zabbixHostData{
				HostID:   iface.HostID,
				HostName: hostname,
			}
		}
	}

	Debug("Filtered host interfaces: %v", hostInterfaces)

	return hostInterfaces
}

func filterHostInterfaceIds(interfaces []zabbix.HostInterface) []string {
	var interfaceIds []string
	for _, iface := range interfaces {
		interfaceIds = append(interfaceIds, iface.InterfaceID)
	}

	Debug("Filtered host interface IDs: %v", interfaceIds)

	return interfaceIds
}

func getItems(z *zabbix.Session, interfaceIds []string, search map[string][]string) []zabbix.Item {
	items, err := z.GetItems(zabbix.ItemGetParams{
		GetParameters: zabbix.GetParameters{
			SearchByAny:               true,
			EnableTextSearchWildcards: true,
			TextSearch:                search,
		},
		InterfaceIDs: interfaceIds,
	})
	handleError("Querying items", err)

	Debug("Items: %v", items)

	return items
}

func filterItems(zh *zabbixHosts, items []zabbix.Item) {
	for _, item := range items {
		hostId := item.HostID

		host, hostPresent := (*zh)[hostId]

		if !hostPresent {
			continue
		}

		host.Metrics = append(host.Metrics, zabbixMetric{
			ID:    item.ItemID,
			Key:   item.ItemKey,
			Name:  item.ItemName,
			Value: item.LastValue,
			Error: item.Error,
		})

		if item.Error != "" {
			host.Error = true
			Error("Item %s (%s) in host %s contains error: %s", item.ItemID, item.ItemName, item.HostID, item.Error)
		}
	}
}

func parseHostMetadata(raw string) (zabbixHostMetaData, bool, error) {
	ok := false
	metadata := make(zabbixHostMetaData)

	err := yaml.Unmarshal([]byte(raw), &metadata)
	Debug("parseHostMetadata() unmarshalled %v", metadata)
	if err != nil {
		return nil, ok, err
	}

	if len(metadata) > 0 {
		ok = true
	}

	return metadata, ok, nil
}

func scanHostMetadata(host *zabbixHostData) {
	for k, v := range host.Meta {
		if k == "label" {
			host.Label = v
			Debug("setHostLabel() set label of host %s (%s) to %s", host.HostID, host.HostName, host.Label)

			return
		}
	}

	// instead of duplicating HostName into Label, maybe better check if Label is empty while processing the host and then fall back to HostName there?
	host.Label = host.HostName

	return
}

func scanHost(host *zabbixHostData) bool {
	have_agent_hostname := false
	have_sys_hw_manufacturer := false
	have_sys_hw_metadata := false

	for _, metric := range host.Metrics {
		Debug("scanHost() processing %s => %s", metric.Key, metric.Value)

		switch metric.Key {

		case "agent.hostname":
			have_agent_hostname = true

		case "sys.hw.manufacturer":
			have_sys_hw_manufacturer = true

			if metric.Value == "QEMU" {
				host.ObjType = "Virtual"
				// TODO: map virtualization cluster
			}

		case "sys.hw.metadata":
			have_sys_hw_metadata = true

			metadata, ok, err := parseHostMetadata(metric.Value)
			if err == nil {
				host.Meta = metadata

				if !ok {
					Warn("Host %s (%s) serves empty metadata", host.HostID, host.HostName)
				}

				scanHostMetadata(host)

				break
			}

			Error("Host %s (%s) serves invalid metadata: %s", host.HostID, host.HostName, err)
			host.Error = true
		}

		if have_agent_hostname && have_sys_hw_manufacturer && have_sys_hw_metadata {
			break
		}
	}

	if !have_agent_hostname {
		Error("Host %s (%s) is missing the 'agent.hostname' item.", host.HostID, host.HostName)
	}

	if !have_sys_hw_manufacturer {
		Error("Host %s (%s) is missing the 'sys.hw.manufacturer' item.", host.HostID, host.HostName)
	}

	if !have_sys_hw_metadata {
		Warn("Host %s (%s) is missing the 'sys.hw.metadata' item.", host.HostID, host.HostName)
	}

	if !have_agent_hostname || !have_sys_hw_manufacturer {
		host.Error = true

		return false
	}

	return true
}
