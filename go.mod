module github.com/SUSE/zabbix-netbox-sync

go 1.23.5

require (
	github.com/fabiang/go-zabbix v1.0.0
	github.com/netbox-community/go-netbox/v4 v4.2.2-2
)

require github.com/hashicorp/go-version v1.6.0 // indirect

replace github.com/fabiang/go-zabbix => github.com/tacerus/go-zabbix v0.0.0-20250220214502-4e001bd52fb4
