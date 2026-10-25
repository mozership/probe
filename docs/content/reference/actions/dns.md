# DNS Action

The `dns` action asks a DNS server for the records of a name, and returns what the server answered. Use it to check that a name resolves to the addresses expected, that the MX, SPF, DKIM and DMARC records a mail flow depends on are in place, or that a change to a zone has reached a name server.

## Basic Syntax

A DNS step gives the name to ask for, and the type of record when it is not an address.

```yaml
steps:
  - name: Where the mail of example.com goes
    uses: dns
    with:
      name: example.com
      type: MX
    test: res.rcode == "NOERROR" && res.answers[0].host == "mail.example.com"
```

## Parameters

| Key | Required | Default | Description |
|---|---|---|---|
| `name` | Yes | | The domain name to ask for. A dot at its end is optional. With `type: PTR`, an IPv4 or IPv6 address may be given, and is asked for as its reverse name |
| `type` | No | `A` | The type of record, in any case: `A`, `AAAA`, `CNAME`, `MX`, `TXT`, `NS`, `SOA`, `SRV`, `PTR`, `CAA`, or any other type DNS has. `AXFR` and `IXFR`, which transfer a zone, are not queries the action sends |
| `server` | No | The resolvers of the machine | The server to ask, as `host` or `host:port`. An IPv6 address is given bare or in brackets, and in brackets with a port |
| `protocol` | No | `udp` | `udp`, `tcp`, or `tls` for DNS over TLS |
| `timeout` | No | `5s` | How long the whole query may take, as a duration such as `500ms` or `10s`, or a number of seconds. It must be above zero |

Without `server`, the action reads the resolvers from `/etc/resolv.conf` and asks them in order until one answers. Each is given an equal part of the `timeout` that is left, so that a resolver that does not answer leaves time for the next: with two resolvers and the default `5s`, the first has 2.5 seconds. The port is 53, or 853 with `protocol: tls`.

An answer cut short over UDP is asked for again over TCP, as `dig` does, and `res.protocol` then says `tcp`. With `protocol: tls`, the certificate of the server is checked against the host given in `server`.

## Response

| Field | Description |
|---|---|
| `res.rcode` | The response code of the server: `NOERROR`, `NXDOMAIN`, `SERVFAIL`, `REFUSED` and so on |
| `res.answers` | The records of the answer, in the order the server gave them |
| `res.values` | The `data` of each answer of the type asked for. An alias the answer came through is left out, so `res.values` of an `A` query holds only addresses |
| `res.authoritative` | `true` when the server answered as the one that holds the zone |
| `res.server` | The server that answered, as `host:port` |
| `res.protocol` | The protocol the answer came over |
| `status` | `0` when `res.rcode` is `NOERROR`, and `1` otherwise |

Each entry of `res.answers` has `name`, `type`, `ttl` and `data`. Host names are given without the dot they end with. The types with several parts have them by name too:

| Type | `data` | Other fields |
|---|---|---|
| `A`, `AAAA` | The address | |
| `CNAME`, `NS`, `PTR` | The host name | |
| `TXT` | The text. A text sent in pieces is joined into one | |
| `MX` | `10 mail.example.com` | `preference`, `host` |
| `SRV` | `10 60 5060 sip.example.com` | `priority`, `weight`, `port`, `target` |
| `SOA` | `ns1.example.com hostmaster.example.com 2026101001 7200 3600 1209600 300` | `ns`, `mbox`, `serial`, `refresh`, `retry`, `expire`, `minimum` |
| `CAA` | `0 issue letsencrypt.org` | `flag`, `tag`, `value` |
| Any other | The record as a zone file writes it | |

A name that exists with no record of the type asked for is `NOERROR` with no answers, as DNS has it.

## Examples

### The Addresses of a Name

```yaml
- name: api.example.com points at the load balancer
  uses: dns
  with:
    name: api.example.com
  test: res.values == ["203.0.113.10"]
```

A name behind an alias answers with the alias and the addresses. `res.answers` holds both, and `res.values` only the addresses:

```yaml
- name: www is an alias of the CDN
  uses: dns
  with:
    name: www.example.com
  test: res.answers[0].type == "CNAME" && res.answers[0].data == "example.cdn.net" && len(res.values) > 0
```

### Mail Records

```yaml
- name: SPF allows the mail service
  uses: dns
  with:
    name: example.com
    type: TXT
  test: 'any(res.values, {# startsWith "v=spf1" && # contains "include:_spf.example.net"})'

- name: DMARC rejects what fails
  uses: dns
  with:
    name: _dmarc.example.com
    type: TXT
  test: res.values[0] contains "p=reject"
```

A test that starts with a quote or holds ` #` is written in quotes, so that YAML does not take `#` for a comment.

### A Change Reached Every Name Server

Ask each name server of the zone itself, and check that it answers as the one that holds it:

```yaml
- name: ns1 serves the new address
  uses: dns
  with:
    name: api.example.com
    server: ns1.example.com
  test: res.authoritative && res.values == ["203.0.113.10"]
```

### A Name That Should Not Exist

A server that answers with an error is a result the test can check, not a failed step:

```yaml
- name: The old name is gone
  uses: dns
  with:
    name: old.example.com
  test: res.rcode == "NXDOMAIN"
```

### The Name of an Address

```yaml
- name: The mail server has a reverse name
  uses: dns
  with:
    name: 203.0.113.25
    type: PTR
  test: res.values == ["mail.example.com"]
```

### DNS over TLS

```yaml
- name: Ask a public resolver over TLS
  uses: dns
  with:
    name: example.com
    server: dns.google
    protocol: tls
  test: res.rcode == "NOERROR" && res.protocol == "tls"
```

## Under a Guard

A query writes nothing, so the action runs under `--read-only` as it is. Run with `--allow-host`, it sends the query only to a server the run allows, at port 53 when `server` names none, or 853 with `protocol: tls`. Without `server`, every resolver of the machine must be allowed, since the query may go to any of them; a step is refused otherwise, and says to give `server`. The name asked for is not a host the action connects to, and is not checked. A refused step fails with the kind `refused`. See [Guard](/guide/concepts/guard).

```bash
probe --allow-host 1.1.1.1 workflow.yml
```

## Error Handling

A server that answers, whatever it answers, gives a result: `res.rcode` says what, and `status` is `1` unless it is `NOERROR`. A step fails with an action error when no server answers, when the timeout runs out, and when a parameter is not valid, such as a type DNS does not have.
