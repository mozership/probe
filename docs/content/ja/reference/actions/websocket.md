# WebSocketアクション

WebSocketアクションは、WebSocketのサーバーに接続し、ステップに並べた順にメッセージを送受信して、受け取ったものを返します。

[外部アクション](/ja/guide/concepts/actions#外部アクション)として[mozership/probe-websocket](https://github.com/mozership/probe-websocket)で公開しています。ワークフローが初めて使うときにProbeがダウンロードし、固定したコミットの`action.yml`が示すSHA-256と一致する実行ファイルだけを実行します。Probe v1.17.0以降が必要です。ガードの下で実行するには、また`probe check`で`with`を検査するには、v1.21.0以降が必要です。

## 基本的な構文

ステップでは40文字のコミットSHAでアクションを固定します。各[リリース](https://github.com/mozership/probe-websocket/releases)のノートの先頭に、コピーして使う`uses`の行があります。

```yaml
steps:
  - name: Subscribe and get an update
    uses: github.com/mozership/probe-websocket@a159244b9ab3f5ec4c61782af1e711bce705c8a8 # v0.1.0
    with:
      url: wss://stream.example.com/ws
      headers:
        Authorization: "Bearer {{vars.token}}"
      messages:
        - send: {type: subscribe, channel: ticker}
        - receive:
            match: {type: subscribed}
        - receive:
            count: 3
            match: {type: update}
    test: res.code == 101 && len(res.messages) == 4 && res.messages[3].data.price > 0
```

ステップごとに新しく接続し、`messages`を順に実行してから閉じます。購読は次のステップに引き継がれないため、一連のやり取りは1つのステップにまとめます。

## パラメータ

| パラメータ | 型 | 必須 | デフォルト | 説明 |
|---|---|---|---|---|
| `url` | String | はい | - | 接続先のサーバー。`ws`または`wss` |
| `headers` | Object | いいえ | - | `Authorization`などのハンドシェイクのリクエストヘッダー。`User-Agent: probe-websocket/<version>`を上書きします |
| `subprotocols` | List | いいえ | - | 提示するサブプロトコル。優先する順に並べます |
| `messages` | List | いいえ | - | 送受信する内容を順に並べたもの。[メッセージ](#メッセージ)を参照してください。省略すると、接続して閉じるだけです |
| `timeout` | Duration | いいえ | `30s` | ハンドシェイクから切断までのステップ全体の制限時間。`10s`のような形式か秒数で指定します。`0`を指定すると制限しません |

これら以外のキーを`with`に書くと、何も送る前にステップが失敗します。`action.yml`はこれらを`params`として宣言しているため、`probe check`がそのキーを行番号とともに報告します。

## メッセージ

`messages`の各エントリには、次のキーのうちちょうど1つを書きます。

| キー | 値 | 動作 |
|---|---|---|
| `send` | 文字列、またはそれ以外の値 | テキストメッセージを送ります。文字列はそのまま、それ以外の値はJSONにして送ります |
| `send_binary` | Base64の文字列 | デコードしたバイト列をバイナリメッセージとして送ります |
| `receive` | Object、または空 | メッセージを待ち、`res.messages`に入れます。オプションがなければ次の1通を受け取ります |

`receive`には次のオプションがあります。

| オプション | 型 | 説明 |
|---|---|---|
| `count` | Integer | 受け取るメッセージの数。既定は`1` |
| `until_close` | Boolean | サーバーが接続を閉じるまで、すべてのメッセージを受け取ります。`count`とは併用できず、最後のエントリにしか書けません |
| `match` | Any | `data`がこの値を含むメッセージだけを受け取り、それ以外は読み飛ばします。Objectは、そのすべてのキーを一致する値とともに持つObjectに一致します。リストは、同じ数の要素がそれぞれ一致するリストに一致します。それ以外の値は等しい値に一致し、数値は値で比べます |

`match`があるとき、`count`は一致したメッセージの数です。読み飛ばしたメッセージは残しません。

## レスポンスオブジェクト

| プロパティ | 型 | 説明 |
|---|---|---|
| `res.code` | Integer | ハンドシェイクのHTTPステータスコード。接続がアップグレードされたときは`101` |
| `res.status` | String | `"101 Switching Protocols"`のようなHTTPステータス行 |
| `res.headers` | Object | 正規化した名前をキーとするハンドシェイクのレスポンスヘッダー |
| `res.subprotocol` | String | サーバーが選んだサブプロトコル。選ばなかったときは空 |
| `res.messages` | Array | `receive`のエントリが受け取ったメッセージ。受け取った順に並びます |
| `res.close` | Object | サーバーが接続を閉じたときの`code`と`reason`。閉じなかったときは`null` |
| `res.error` | String | 最後のエントリより前でメッセージの処理が止まった理由。すべてのエントリを終えたときは空 |
| `req` | Object | 使った`url`、`headers`、`subprotocols`、`messages` |
| `rt` | Duration | ハンドシェイクから切断までの時間 |
| `status` | Integer | 接続がアップグレードされ、すべてのエントリを終えたら`0`。それ以外は`1` |

`res.messages`の各メッセージには、次のプロパティがあります。

| プロパティ | 型 | 説明 |
|---|---|---|
| `type` | String | `"text"`または`"binary"` |
| `data` | Any | テキストメッセージは、JSONなら解析した値、それ以外は文字列。バイナリメッセージはBase64 |
| `raw` | String | 解析前のテキスト、またはバイナリメッセージのBase64 |

サーバーがハンドシェイクに応答した後のことは、すべて結果として扱います。そのため、`401`で拒否されたハンドシェイク、期待したメッセージより前の切断、届かなかったメッセージもテストで確かめられます。メッセージの処理は実行できない最初のエントリで止まり、`res.error`がそのエントリを示します。たとえば`messages[1] took 2 of 3 messages: timed out after 10s`のようになります。接続の拒否やタイムアウトのように、ハンドシェイクに応答を得られなかったときだけ、ステップをエラーとして失敗させます。

すべてのエントリを終えた時点でサーバーが接続を閉じていなければ、アクションが`1000`で閉じます。16 MiBを超えるメッセージを受け取ると、やり取りはそこで終わります。

```yaml
steps:
  - name: The feed sends two updates, then closes
    uses: github.com/mozership/probe-websocket@a159244b9ab3f5ec4c61782af1e711bce705c8a8 # v0.1.0
    with:
      url: wss://stream.example.com/replay
      messages:
        - receive:
            until_close: true
            match: {type: update}
    test: len(res.messages) == 2 && res.close.code == 1000
```

## ガードの下での動作

このアクションは実行のガードに従い、`action.yml`で`guard: [read-only, allow-host]`を宣言しています。そのため、どちらのガードの下でも`--allow-action`なしで実行します。拒否したステップは、接続する前に種類`refused`で失敗します。

- `--read-only`の下では、受信だけのステップを実行します。メッセージがサーバーで何をするかは判断できないため、`send`か`send_binary`のエントリを含むステップは拒否します。購読の申し込みのように、何かを送らないとメッセージを返さないサーバーには、`--allow-action`で許可が必要です。
- `--allow-host`の下では、`url`のホストと、ハンドシェイクの各リダイレクト先のホストが、実行が許可するものでなければなりません。ポートのないURLは、スキームの既定のポート（`ws`は`80`、`wss`は`443`）として扱います。

[ガード](/ja/guide/concepts/guard)を参照してください。

## 関連項目

- **[外部アクション](/ja/guide/concepts/actions#外部アクション)** - Probeが外部アクションを解決し、照合する仕組み
- **[HTTP](/ja/reference/actions/http)** - 通常のHTTPリクエスト
- **[GraphQL](/ja/reference/actions/graphql)** - HTTPでのGraphQLのクエリ
