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
	"strconv"
	"strings"
)

type zabbixMetric struct {
	ID    string
	Key   string
	Name  string
	Value string
	Error string
}

type linuxInterface struct {
	Name      string
	Addresses []string
	Type      int64
}

type linuxInterfaces map[string]*linuxInterface
type zabbixHostMetaData map[string]string

type zabbixHostData struct {
	HostID     string
	HostName   string
	Metrics    []zabbixMetric
	Error      bool
	ObjType    string
	Meta       zabbixHostMetaData
	Label      string
	Interfaces linuxInterfaces
	CPUs       float64
	Memory     int32
}

type zabbixHosts map[string]*zabbixHostData

func getInterface(host *zabbixHostData, name string) *linuxInterface {
	_, exists := host.Interfaces[name]
	if !exists {
		host.Interfaces[name] = new(linuxInterface)
	}

	return host.Interfaces[name]
}

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
			error := false
			if hostname == "" {
				Error("Empty DNS field in interface %s on host %s (%s)", iface.InterfaceID, iface.HostID, iface.IP)
				hostname = iface.IP
				error = true

			}
			(*zh)[iface.HostID] = &zabbixHostData{
				HostID:   iface.HostID,
				HostName: hostname,
				Error:    error,
			}
		}
	}

	Debug("Filtered host interfaces: %v", hostInterfaces)

	return hostInterfaces
}

func getItems(z *zabbix.Session, hostIds []string, search map[string][]string) []zabbix.Item {
	items, err := z.GetItems(zabbix.ItemGetParams{
		GetParameters: zabbix.GetParameters{
			SearchByAny:               true,
			EnableTextSearchWildcards: true,
			TextSearch:                search,
		},
		HostIDs: hostIds,
	})
	handleError("Querying items", err)

	Debug("Items: %v", items)

	return items
}

func filterItems(zh *zabbixHosts, items []zabbix.Item, keys []string) {
	for _, item := range items {
		//if !contains(keys, item.ItemKey) {
		//	Debug("Discarding item with key %s", item.ItemKey)

		//	continue
		//}
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
			Error("Item %s (%s) in host %s contains error: '%s'", item.ItemID, item.ItemName, item.HostID, item.Error)
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
			Debug("scanHostMetadata() set label of host %s (%s) to %s", host.HostID, host.HostName, host.Label)

			return
		}
	}

	// instead of duplicating HostName into Label, maybe better check if Label is empty while processing the host and then fall back to HostName there?
	host.Label = host.HostName

	return
}

func detectInterfaceMetric(name string) bool {
	return strings.HasPrefix(name, "net.if.")
}

func detectFileContentsMetric(name string) bool {
	return strings.HasPrefix(name, "vfs.file.contents[")
}

func detectInterfaceTypePath(name string) bool {
	return strings.HasPrefix(name, "/sys/class/net/") && strings.HasSuffix(name, "/type")
}

func parseInterfaceMetric(zm zabbixMetric) (string, string, []string) {
	// dissect net.if.ip4["eth2"] without regex
	mkeysplit := strings.SplitN(strings.Replace(zm.Key, "]", "", 1), "[", 2)
	Debug("parseInterfaceMetric() split to %+v", mkeysplit)
	family := strings.SplitN(mkeysplit[0], ".", 3)[2]
	name := strings.Replace(strings.Replace(mkeysplit[1], "\"", "", 2), "\"", "", 2)
	Debug("parseInterfaceMetric() set family \"%s\" name \"%s\"", family, name)
	addresses := strings.Split(zm.Value, "\n")

	return name, family, addresses
}

func parseFileContentsMetric(zm zabbixMetric) (string, string) {
	// dissect vfs.file.contents["/sys/class/net/eth2/type"] without regex
	basekey := strings.SplitN(strings.Replace(zm.Key, "]", "", 1), "[", 2)
	indexkey := strings.Replace(basekey[1], "\"", "", 2)
	value := zm.Value

	return indexkey, value
}

func parseInterfaceTypePath(path string) string {
	// /sys/class/net/eth2/type => eth2
	return strings.SplitN(path, "/", 6)[4]
}

func scanHost(host *zabbixHostData) bool {
	have_agent_hostname := false
	have_sys_hw_manufacturer := false
	have_sys_hw_metadata := false

	host.Interfaces = make(linuxInterfaces)

	for _, metric := range host.Metrics {
		Debug("scanHost() processing %s => %s", metric.Key, metric.Value)

		mkey := metric.Key

		if detectInterfaceMetric(mkey) {
			name, _, addresses := parseInterfaceMetric(metric)
			if len(addresses) == 0 {
				Debug("scanHost() ignoring host %s interface %s due to nil address", host.HostName, name)
			} else {
				inf := getInterface(host, name)
				inf.Addresses = append(inf.Addresses, addresses...)
			}

			continue
		}

		if detectFileContentsMetric(mkey) {
			key, value := parseFileContentsMetric(metric)
			if detectInterfaceTypePath(key) {
				name := parseInterfaceTypePath(key)

				i, err := strconv.ParseInt(value, 10, 32)
				if err != nil {
					Error("Host %s (%s) serves invalid value for interface type", host.HostID, host.HostName)
					continue
				}

				inf := getInterface(host, name)
				inf.Type = i
			}

			continue
		}

		switch mkey {

		case "agent.hostname":
			have_agent_hostname = true

		case "sys.hw.manufacturer":
			have_sys_hw_manufacturer = true

			if metric.Value == "QEMU" {
				host.ObjType = "Virtual"
				// TODO: map virtualization cluster
			} else {
				// assuming all non-QEMU values to be physical is not ideal
				// is there a better item than sys.hw.manufacturer for this ?
				host.ObjType = "Physical"
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

		case "system.cpu.num":
			cpus, err := strconv.ParseFloat(metric.Value, 64)
			if err == nil {
				host.CPUs = cpus
			} else {
				Error("Host %s (%s) serves invalid value for \"sys.cpu.count\": %s - conversion to float failed: %s", host.HostID, host.HostName, metric.Value, err)
			}

		case "vm.memory.size[total]":
			memory_b, err := strconv.ParseInt(metric.Value, 10, 64)
			if err == nil {
				// for NB, memory needs to be in Megabytes
				// should there be some check whether the calculated MB value actually fits into 32 bit?
				memory_mb := int32(float64(memory_b) / (1 << 20))
				Debug("Converted memory %d to %d", memory_b, memory_mb)
				host.Memory = memory_mb
			} else {
				Error("Host %s (%s) serves invalid value for \"vm.memory.size\": %s - conversion to integer failed: %s", host.HostID, host.HostName, metric.Value, err)
			}

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

func scanHosts(zh *zabbixHosts) {
	for _, host := range *zh {
		if host.Error {
			Debug("Skipping preprocessing of host %s.", host.HostName)

			continue
		}

		Debug("Preprocessing host %s", host.HostName)

		ok := scanHost(host)

		if !ok {
			Debug("Scan of host %s (%s) returned errors.", host.HostID, host.HostName)

			continue
		}
	}
}
