# DNSアクション

`dns`アクションは、名前のレコードをDNSサーバーに問い合わせ、サーバーの応答を返します。名前が期待どおりのアドレスに解決されること、メールの流れが依存するMX、SPF、DKIM、DMARCのレコードが揃っていること、ゾーンの変更がネームサーバーに届いたことなどを確認できます。

## 基本構文

DNSのステップには、問い合わせる名前と、アドレス以外を問い合わせる場合はレコードの種類を指定します。

```yaml
steps:
  - name: Where the mail of example.com goes
    uses: dns
    with:
      name: example.com
      type: MX
    test: res.rcode == "NOERROR" && res.answers[0].host == "mail.example.com"
```

## パラメータ

| キー | 必須 | デフォルト | 説明 |
|---|---|---|---|
| `name` | はい | | 問い合わせるドメイン名。末尾のドットは省略できます。`type: PTR`ではIPv4またはIPv6のアドレスも指定でき、その逆引きの名前として問い合わせます |
| `type` | いいえ | `A` | レコードの種類。大文字小文字は問いません。`A`、`AAAA`、`CNAME`、`MX`、`TXT`、`NS`、`SOA`、`SRV`、`PTR`、`CAA`のほか、DNSにある種類を指定できます。ゾーン転送の`AXFR`と`IXFR`は送りません |
| `server` | いいえ | マシンのリゾルバ | 問い合わせ先のサーバー。`host`または`host:port`。IPv6アドレスはそのまま、または角括弧で囲んで指定し、ポートを付けるときは角括弧で囲みます |
| `protocol` | いいえ | `udp` | `udp`、`tcp`、またはDNS over TLSの`tls` |
| `timeout` | いいえ | `5s` | 問い合わせ全体にかけてよい時間。`500ms`や`10s`のような期間、または秒数。0より大きい値 |

`server`を省略すると、`/etc/resolv.conf`からリゾルバを読み、応答があるまで順に問い合わせます。各リゾルバには`timeout`の残り時間を等分して割り当てるので、応答しないリゾルバがあっても次に問い合わせる時間が残ります。リゾルバが2つでデフォルトの`5s`なら、最初のリゾルバに2.5秒です。ポートは53、`protocol: tls`では853です。

UDPで途中までしか返らなかった応答は、`dig`と同じくTCPで問い合わせ直し、そのとき`res.protocol`は`tcp`になります。`protocol: tls`では、サーバーの証明書を`server`に指定したホストに対して検証します。

## レスポンス

| フィールド | 説明 |
|---|---|
| `res.rcode` | サーバーの応答コード。`NOERROR`、`NXDOMAIN`、`SERVFAIL`、`REFUSED`など |
| `res.answers` | 応答のレコード。サーバーが返した順 |
| `res.values` | 問い合わせた種類の各レコードの`data`。途中の別名は含まないので、`A`の問い合わせの`res.values`はアドレスだけになります |
| `res.authoritative` | サーバーがそのゾーンを持つ権威サーバーとして応答したとき`true` |
| `res.server` | 応答したサーバー。`host:port` |
| `res.protocol` | 応答を受け取ったプロトコル |
| `status` | `res.rcode`が`NOERROR`なら`0`、それ以外は`1` |

`res.answers`の各要素には`name`、`type`、`ttl`、`data`があります。ホスト名は末尾のドットを付けずに返します。複数の部分からなる種類は、それぞれを名前でも参照できます。

| 種類 | `data` | そのほかのフィールド |
|---|---|---|
| `A`、`AAAA` | アドレス | |
| `CNAME`、`NS`、`PTR` | ホスト名 | |
| `TXT` | テキスト。分割して送られたテキストは1つにつなげます | |
| `MX` | `10 mail.example.com` | `preference`、`host` |
| `SRV` | `10 60 5060 sip.example.com` | `priority`、`weight`、`port`、`target` |
| `SOA` | `ns1.example.com hostmaster.example.com 2026101001 7200 3600 1209600 300` | `ns`、`mbox`、`serial`、`refresh`、`retry`、`expire`、`minimum` |
| `CAA` | `0 issue letsencrypt.org` | `flag`、`tag`、`value` |
| そのほか | ゾーンファイルでの表記 | |

名前はあるが問い合わせた種類のレコードがない場合は、DNSの仕様どおり`NOERROR`で、レコードは空です。

## 例

### 名前のアドレス

```yaml
- name: api.example.com points at the load balancer
  uses: dns
  with:
    name: api.example.com
  test: res.values == ["203.0.113.10"]
```

別名を挟む名前は、別名とアドレスの両方を返します。`res.answers`には両方が入り、`res.values`にはアドレスだけが入ります。

```yaml
- name: www is an alias of the CDN
  uses: dns
  with:
    name: www.example.com
  test: res.answers[0].type == "CNAME" && res.answers[0].data == "example.cdn.net" && len(res.values) > 0
```

### メールのレコード

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

引用符で始まるテストや` #`を含むテストは、YAMLが`#`をコメントとして扱わないように、全体を引用符で囲みます。

### 変更がすべてのネームサーバーに届いたか

ゾーンの各ネームサーバーに直接問い合わせ、権威サーバーとして応答していることを確かめます。

```yaml
- name: ns1 serves the new address
  uses: dns
  with:
    name: api.example.com
    server: ns1.example.com
  test: res.authoritative && res.values == ["203.0.113.10"]
```

### 存在しないはずの名前

サーバーがエラーで応答した場合も、ステップの失敗ではなく、テストで確認できる結果になります。

```yaml
- name: The old name is gone
  uses: dns
  with:
    name: old.example.com
  test: res.rcode == "NXDOMAIN"
```

### アドレスの名前

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

## ガードの下で

問い合わせは何も書き込まないので、`--read-only`の下でもそのまま実行されます。`--allow-host`の下では、実行が許可するサーバーにだけ問い合わせを送ります。ポートは、`server`に指定がなければ53、`protocol: tls`では853として照合します。`server`を省略した場合は、問い合わせがどのリゾルバにも送られうるため、マシンのすべてのリゾルバが許可されている必要があります。そうでなければステップは拒否され、拒否のメッセージには`server`を指定するように書かれます。問い合わせる名前はアクションが接続するホストではないので、照合しません。拒否されたステップは種類`refused`で失敗します。[ガード](/ja/guide/concepts/guard)を参照してください。

```bash
probe --allow-host 1.1.1.1 workflow.yml
```

## エラー処理

サーバーが応答すれば、その内容にかかわらず結果になります。`res.rcode`が内容を示し、`NOERROR`でなければ`status`は`1`です。どのサーバーも応答しないとき、タイムアウトしたとき、DNSにない種類などパラメータが正しくないときは、ステップはアクションエラーで失敗します。
