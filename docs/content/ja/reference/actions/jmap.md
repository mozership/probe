# JMAPアクション

JMAPアクションは、[JMAP](https://jmap.io/)（[RFC 8620](https://www.rfc-editor.org/rfc/rfc8620)、[RFC 8621](https://www.rfc-editor.org/rfc/rfc8621)）のメソッドを呼びます。セッションを取得し、各呼び出しのアカウントを補い、呼び出しを1回のリクエストで送ります。レスポンスは呼び出しのidごとに返し、メソッドのエラーは1つのリストにまとめます。

[外部アクション](/ja/guide/concepts/actions#外部アクション)として[mozership/probe-jmap](https://github.com/mozership/probe-jmap)で公開しています。ワークフローが初めて使うときにProbeがダウンロードし、固定したコミットの`action.yml`が示すSHA-256と一致する実行ファイルだけを実行します。Probe v1.21.0以降が必要です。このバージョンから、`action.yml`が宣言するガードとパラメータを読みます。

## 基本的な構文

ステップでは40文字のコミットSHAでアクションを固定します。各[リリース](https://github.com/mozership/probe-jmap/releases)のノートの先頭に、コピーして使う`uses`の行があります。

```yaml
steps:
  - name: Read the latest message
    uses: github.com/mozership/probe-jmap@52000030f1bd839677830d1ab07a024183f581b7 # v0.2.0
    with:
      url: https://jmap.example.com
      basic_auth:
        username: "{{vars.user}}"
        password: "{{vars.pass}}"
      calls:
        - method: Email/query
          id: latest
          args:
            sort: [{property: receivedAt, isAscending: false}]
            limit: 1
        - method: Email/get
          id: email
          args:
            "#ids": {resultOf: latest, path: /ids}
            properties: [subject, from, receivedAt]
    test: status == 0 && len(res.results.email.list) == 1
    echo: "Latest: {{res.results.email.list[0].subject}}"
```

## パラメータ

| パラメータ | 型 | 必須 | デフォルト | 説明 |
|---|---|---|---|---|
| `url` | String | はい | - | サーバー。`http`または`https`。パスのないURLはサーバーとして扱い、セッションを`/.well-known/jmap`から取得します。パスのあるURLはセッションのURLそのものとして扱います |
| `calls` | Array | はい | - | メソッドの呼び出し。この順に1回のリクエストで送ります |
| `using` | Array | いいえ | 推定 | リクエストが使うcapability。既定ではcoreと、`calls`のメソッドを定義するcapabilityです |
| `account_id` | String | いいえ | プライマリアカウント | `args`に`accountId`がない呼び出しのアカウント |
| `basic_auth` | Object | いいえ | - | HTTP Basic認証の`username`と`password` |
| `headers` | Object | いいえ | - | `Authorization: Bearer <token>`などのリクエストヘッダー |
| `timeout` | Duration | いいえ | `30s` | セッションとAPIのリクエストを合わせた制限時間。`10s`のような形式か秒数で指定します。`0`を指定すると制限しません |

`calls`の各呼び出しには次を書きます。

| フィールド | 必須 | 説明 |
|---|---|---|
| `method` | はい | `Email/get`のようなメソッド名 |
| `args` | いいえ | メソッドの引数 |
| `id` | いいえ | 呼び出しのid。back-referenceと`res.results`で使います。既定は`c`と呼び出しの位置（`c0`から）です |

`args`に`accountId`も`#accountId`もない呼び出しは、`account_id`のアカウントで行います。`account_id`もなければ、セッションがメソッドのcapabilityに示すプライマリアカウントで行います。`Core/echo`はアカウントなしで行います。

名前が`#`で始まる引数（back-reference）では、`name`を省略できます。省略すると、`resultOf`が指す呼び出しのメソッドを補います。back-referenceは引数全体を置き換えるため、`filter`の中のように引数の内側には書けません。

### capability

`using`はメソッド名の型から推定します。

| 型 | capability |
|---|---|
| `Core` | `urn:ietf:params:jmap:core` |
| `Mailbox`、`Thread`、`Email`、`SearchSnippet` | `urn:ietf:params:jmap:mail` |
| `Identity`、`EmailSubmission` | `urn:ietf:params:jmap:submission` |
| `VacationResponse` | `urn:ietf:params:jmap:vacationresponse` |
| `MDN` | `urn:ietf:params:jmap:mdn` |
| `Blob` | `urn:ietf:params:jmap:blob` |
| `Quota` | `urn:ietf:params:jmap:quota` |
| `SieveScript` | `urn:ietf:params:jmap:sieve` |
| `AddressBook`、`ContactCard` | `urn:ietf:params:jmap:contacts` |

これ以外の型のメソッドには`using`が必要です。`using`を指定したときは、書いたとおりに送ります。

上記のパラメータ以外のキーを`with`に書くと、何も送る前にステップが失敗します。`action.yml`はパラメータを`params`として宣言しているため、`probe check`がそのキーを行番号とともに報告します。

## レスポンスオブジェクト

| プロパティ | 型 | 説明 |
|---|---|---|
| `res.code` | Integer | HTTPステータスコード |
| `res.status` | String | `"200 OK"`のようなHTTPステータス行 |
| `res.headers` | Object | 正規化した名前をキーとするレスポンスヘッダー |
| `res.session` | Object | サーバーが返したセッションオブジェクト。ないときは`null` |
| `res.results` | Object | 各メソッドのレスポンスの引数。呼び出しのidをキーにします。メソッドのエラーもエラーオブジェクトとしてここに入ります |
| `res.responses` | Array | すべてのメソッドのレスポンス。順に`name`、`args`、`id`を持ちます |
| `res.errors` | Array | 失敗したもの。内容は下記のとおりです。何も失敗しなければ空です |
| `res.body` | Any | レスポンスボディ全体。JSONなら解析した値、それ以外は文字列 |
| `res.rawbody` | String | 解析前のボディ。ボディがJSONのときに入ります |
| `req` | Object | `session_url`、APIの`url`、`using`、送った`calls`、`headers` |
| `rt` | Duration | セッションとAPIのリクエストを合わせた時間 |
| `status` | Integer | APIが2xxでメソッドのレスポンスを返し、`res.errors`が空なら`0`。それ以外は`1` |

JMAPのサーバーは、実行できなかったメソッドにもHTTP 200で応答します。そのため、`res.code`だけでは呼び出しが成功したかわかりません。`res.errors`にはすべての失敗が入ります。各要素は、サーバーが返したエラーオブジェクトに次のフィールドを加えたものです。

| `kind` | 失敗したもの | 加えるフィールド |
|---|---|---|
| `method` | `error`のレスポンスが返ったメソッドの呼び出し | `id`、`method` |
| `notCreated`、`notUpdated`、`notDestroyed` | `/set`の呼び出しのレコード | `id`、`method`、`key`（作成のidかレコードのid） |
| `request` | problem detailsのオブジェクトが返ったリクエスト全体 | - |
| `session` | サーバーが返さなかったセッション | `description` |

サーバーがセッションを返さなかったときは、メソッドの呼び出しを送りません。このとき`res`はセッションのリクエストへのレスポンスです。

サーバーが返したレスポンスはすべて結果として扱います。そのため、メソッドのエラーや401もテストで確かめられます。接続の拒否やタイムアウトのように、レスポンスを得られなかったリクエストだけが、ステップをエラーとして失敗させます。

## 使用例

### メッセージを送り、届くのを待つ

`Email/set`でメッセージを作り、`EmailSubmission/set`で送ります。送るメッセージは作成のid `#msg`で指します。続くステップでは`retry`で届くのを待ちます。

```yaml
steps:
  - name: Send a message to bob
    uses: github.com/mozership/probe-jmap@52000030f1bd839677830d1ab07a024183f581b7 # v0.2.0
    with:
      url: "{{vars.url}}"
      basic_auth: {username: "{{vars.alice}}", password: "{{vars.alice_pass}}"}
      calls:
        - method: Email/set
          id: create
          args:
            create:
              msg:
                mailboxIds:
                  "{{outputs.alice.sent}}": true
                from: [{email: "{{vars.alice}}"}]
                to: [{email: "{{vars.bob}}"}]
                subject: "{{vars.subject}}"
                bodyValues: {body: {value: Sent by probe.}}
                textBody: [{partId: body, type: text/plain}]
        - method: EmailSubmission/set
          id: submit
          args:
            create:
              sub:
                identityId: "{{outputs.alice.identity}}"
                emailId: "#msg"
    test: status == 0 && res.results.submit.created.sub.id != nil

  - name: Wait for it in bob's mailbox
    uses: github.com/mozership/probe-jmap@52000030f1bd839677830d1ab07a024183f581b7 # v0.2.0
    with:
      url: "{{vars.url}}"
      basic_auth: {username: "{{vars.bob}}", password: "{{vars.bob_pass}}"}
      calls:
        - method: Email/query
          id: query
          args:
            filter: {subject: "{{vars.subject}}"}
        - method: Email/get
          id: get
          args:
            "#ids": {resultOf: query, path: /ids}
            properties: [subject, receivedAt]
    retry:
      max_attempts: 30
      interval: 1s
    test: status == 0 && len(res.results.get.list) == 1
```

### 失敗を確かめる

```yaml
steps:
  - name: A record that cannot be created
    uses: github.com/mozership/probe-jmap@52000030f1bd839677830d1ab07a024183f581b7 # v0.2.0
    with:
      url: "{{vars.url}}"
      basic_auth: {username: "{{vars.user}}", password: "{{vars.pass}}"}
      calls:
        - method: Email/set
          args:
            create:
              bad:
                mailboxIds: {no-such-mailbox: true}
    test: |
      status == 1 &&
      res.errors[0].kind == "notCreated" &&
      res.errors[0].key == "bad"
```

## ガード

`--read-only`を指定した実行では、`/get`、`/query`、`/changes`、`/queryChanges`、`/lookup`、`/echo`以外のメソッドを含むステップを、何も送る前に拒否します。`--allow-host`が許可しないホストは、セッション、APIのURL、リダイレクト先のいずれでも拒否します。`action.yml`は`guard: [read-only, allow-host]`を宣言しているため、どちらのガードの下でも`--allow-action`なしで実行します。[ガード](/ja/guide/concepts/guard)を参照してください。

## 関連項目

- **[外部アクション](/ja/guide/concepts/actions#外部アクション)** - Probeが外部アクションを解決し、照合する仕組み
- **[IMAP](/ja/reference/actions/imap)** - IMAPでのメールボックスの操作
- **[SMTP](/ja/reference/actions/smtp)** - サーバーへのメールの配送
- **[GraphQL](/ja/reference/actions/graphql)** - Probeとあわせて公開しているもう1つの外部アクション
