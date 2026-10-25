# SMTPアクション

`smtp`アクションはSMTPサーバーへメールを配送します。任意の本文を書いて通知を送るためのものではなく、配送そのものを計測・負荷試験するためのアクションです。本文は自動生成され、サイズは`length`で指定します。

## 基本的な構文

SMTPのステップでは、サーバー、エンベロープのアドレス、送信内容を指定します。

```yaml
steps:
  - name: Send a probe mail
    uses: smtp
    with:
      addr: "localhost:2525"
      from: "sender@example.com"
      to: "recipient@example.com"
      subject: "Delivery probe"
      session: 1
      message: 1
      length: 500
    test: res.code == 0 && res.sent > 0
```

## パラメータ

以下のフィールドで配送内容を指定します。いずれもテンプレート式を書けます。

| パラメータ | 型 | 必須 | デフォルト | 説明 |
|---|---|---|---|---|
| `addr` | String | 必須 | - | SMTPサーバー（`host:port`） |
| `from` | String | 必須 | - | エンベロープの送信者 |
| `to` | String | 必須 | - | エンベロープの受信者。複数の受信者は`a@example.com, b@example.com`のようにカンマで区切る |
| `subject` | String | 任意 | `""` | 件名 |
| `myhostname` | String | 任意 | 実行マシンのホスト名。取得できなければ`localhost` | `EHLO` / `HELO`で名乗るホスト名。`localhost`を名乗るクライアントを拒否するサーバーが多いため、サーバーが受け付ける名前を指定する |
| `session` | Integer | 任意 | `1` | 同時に開くSMTPセッション数。1から1000まで |
| `message` | Integer | 任意 | `1` | 全セッション合計の送信通数。セッションに振り分けて送る。1以上 |
| `length` | Integer | 任意 | `0` | 生成する本文の末尾に付ける`*`の文字数。104857600（100 MiB）まで |
| `starttls` | String | 任意 | `auto` | `auto`はサーバーが提示していれば`STARTTLS`でTLSに切り替え、`required`は提示しないサーバーのセッションを失敗させ、`off`は使わない |
| `insecure_skip_tls` | Boolean | 任意 | `false` | `STARTTLS`の後、自己署名などどんな証明書も受け入れる |
| `timeout` | Duration | 任意 | `5m` | 接続と、サーバーとの読み書きでの1回ごとの待ち時間の上限。メッセージの終わりへの応答はその2倍まで待つ |

`STARTTLS`の後はサーバーの証明書を`addr`のホストで検証するため、自己署名の証明書やホストの合わない証明書は、`insecure_skip_tls`を指定しない限りセッションを失敗させます。ポート465のように最初からTLSで始まる接続には対応していません。認証、CC/BCC、任意の本文やHTMLを指定するパラメータはありません。実行結果にレポートを出したい場合はステップの`echo`を使います。

`timeout`には`10s`や`1m30s`のような時間の文字列か、秒数を書きます。時間でも数でもない値や、0以下の値はエラーになります。デフォルトはRFC 5321（4.5.3.2節）に従います。RFC 5321は、クライアントがほとんどの応答を5分待ち、メッセージの終わりへの応答は10分待つよう求めています。サーバーが応答の前にメッセージを検査することがあるためです。そのため、メッセージの終わりへの応答は常に`timeout`の2倍まで待ちます。この応答が時間切れになったメッセージは、サーバーが受け取っていても失敗として数えます。`timeout`はセッション全体ではなく1回ごとの待ちに掛かるため、サーバーが応答し続ける限り、多くのメッセージを送るセッションは`timeout`より長くかかっても構いません。時間内に応答しないサーバーのセッションは失敗し、1通も配送できなかった場合は`mail delivery failed: tcp dial error: read tcp ...: i/o timeout (the server did not answer within 5m0s)`のようなエラーでステップが終わります。セッション全体の上限は、ステップ自体の`timeout`（デフォルトは5m）です。待ちがどう進んでもアクションはそこで止まるため、セッションをそれより長く続けるなら、ステップの`timeout`を延ばします。

生成するメッセージには`From`、`To`、`Date`、`Subject`のヘッダーが付きます。本文は`This is a test mail.`の後に`length`個の`*`を続けたもので、80文字ごとに改行します。送るときには、メッセージごとにこれらの前へ2つのヘッダーを足します。送信した時刻をUnixナノ秒で表す`X-Send-Timestamp-Ns`と、メッセージごとの`Message-ID`です。`Message-ID`の`@`より前の部分は、件名の末尾にも` - <id>`の形で付けます。

## レスポンスオブジェクト

実行後、`res`に配送の結果が入ります。

| フィールド | 型 | 説明 |
|---|---|---|
| `res.code` | Integer | 失敗したセッションがなく、1通以上配送できたとき`0` |
| `res.sent` | Integer | サーバーが受け付けた通数。途中で失敗したセッションが、それまでに配送した分も含む |
| `res.failed` | Integer | 失敗したセッション数 |
| `res.total` | Integer | 試行した通数。`message`と同じ値 |
| `res.error` | String | 失敗時のエラーメッセージ |
| `res.maildata` | String | サーバーが最後に受け付けたメール。`Message-ID`と送信時刻を含む、送ったままの内容。セッションが複数なら、最後に終わったセッションが最後に送ったメール。1通も受け付けられなければ空 |
| `rt.duration` | String | 配送にかかった時間（例: `"3.4ms"`） |
| `rt.sec` | Float | 配送にかかった時間（秒） |
| `status` | Integer | `res.code`と同じ値 |

サーバーがメールを拒否した場合（`RCPT TO`への`550`など）はサーバーが応答しているので、ステップはそのままテストに進みます。このとき`res.code`は`1`になり、応答は`res.error`に入ります。どのセッションもサーバーに接続できなかった場合は、エラーとしてステップが終わります。

## 使用例

以下の例では、生成したメッセージを送信し、その件数を後続のステップから参照します。

### 複数セッション・複数通の配送

`message`は合計の通数で、セッションに振り分けられます。この例では、一方のセッションが2通、もう一方が1通を配送します。

```yaml
steps:
  - name: Deliver 3 messages over 2 sessions
    id: bulk
    uses: smtp
    with:
      addr: "{{vars.smtp_addr}}"
      from: "{{vars.from_addr}}"
      to: "{{vars.to_addr}}"
      subject: "Bulk delivery test"
      myhostname: probe-client.local
      session: 2
      message: 3
      length: 750
    test: res.code == 0 && res.sent == 3
    outputs:
      sent: res.sent
      elapsed: rt.duration
```

### 結果をレポートする

出力として取り出した件数は、後続のステップから出力できます。

```yaml
  - name: Delivery summary
    uses: hello
    echo: |
      Sent: {{outputs.bulk.sent}}
      Took: {{outputs.bulk.elapsed}}
```

## 関連項目

- **[Mail Latency](/ja/reference/actions/mail-latency)** - 配送遅延の計測
- **[IMAP](/ja/reference/actions/imap)** - メールボックスの操作
- **[YAML設定](/ja/reference/yaml-configuration)** - ステップのプロパティ
