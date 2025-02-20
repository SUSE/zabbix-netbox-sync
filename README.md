# zabbix-netbox-sync (work in progress)

Tool to populate NetBox from Zabbix.

It retrieves data from Zabbix based on a filter configuration and creates or updates matching objects in NetBox.

## Usage

```
$ zabbix-netbox-sync -netbox https://netbox.example.com -zabbix https://zabbix.example.com
```

### Authentication

The following environment variables can be used to make the tool authenticate with the provided NetBox and Zabbix instances:

- `NETBOX_TOKEN` - if not defined, the tool will connect to NetBox anonymously
- `ZABBIX_USER` - if not defined, the tool will default to the Zabbix username "guest"
- `ZABBIX_PASSPHRASE` - if not defined, the tool will connect to Zabbix anonymously
