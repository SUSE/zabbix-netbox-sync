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
	"github.com/netbox-community/go-netbox/v4"
	"net/http"
	"os"
)

func nbConnect(url string, token string) (*netbox.APIClient, context.Context) {
	return netbox.NewAPIClientFor(url, token), context.Background()
}

func getVirtualMachines(nb *netbox.APIClient, ctx context.Context) *netbox.PaginatedVirtualMachineWithConfigContextList {
	result, _, err := nb.VirtualizationAPI.VirtualizationVirtualMachinesList(ctx).Execute()
	handleError("Querying virtual machines", err)

	Debug("getVirtualMachines() returns %v", result.Results)

	return result
}

func getDevices(nb *netbox.APIClient, ctx context.Context) *netbox.PaginatedDeviceWithConfigContextList {
	result, _, err := nb.DcimAPI.DcimDevicesList(ctx).Execute()
	handleError("Querying devices", err)

	Debug("getDevices() returns %v", result.Results)

	return result
}

func handleResponse(created interface{}, response *http.Response, err error) {
	if err != nil {
		Error("API returned: %s", err)
	}

	var body interface{}
	jerr := json.NewDecoder(response.Body).Decode(&body)
	handleError("Decoding response body", jerr)

	if body != nil {
		if err == nil {
			Debug("%+v", body)
		} else {
			Error("%+v", body)
		}
	}

	if err != nil || jerr != nil {
		os.Exit(1)
	}

	Debug("Returned object: %+v", created)
}
