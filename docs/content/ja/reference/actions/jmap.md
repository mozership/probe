# JMAPアクション

JMAPアクションは、[JMAP](https://jmap.io/)（[RFC 8620](https://www.rfc-editor.org/rfc/rfc8620)、[RFC 8621](https://www.rfc-editor.org/rfc/rfc8621)）のメソッドを呼びます。セッションを取得し、各呼び出しのアカウントを補い、呼び出しを1回のリクエストで送ります。レスポンスは呼び出しのidごとに返し、メソッドのエラーは1つのリストにまとめます。呼び出しを書かないステップは、セッションだけを取得します。

[外部アクション](/ja/guide/concepts/actions#外部アクション)として[mozership/probe-jmap](https://github.com/mozership/probe-jmap)で公開しています。ワークフローが初めて使うときにProbeがダウンロードし、固定したコミットの`action.yml`が示すSHA-256と一致する実行ファイルだけを実行します。Probe v1.21.0以降が必要です。このバージョンから、`action.yml`が宣言するガードとパラメータを読みます。

## 基本的な構文

ステップでは40文字のコミットSHAでアクションを固定します。各[リリース](https://github.com/mozership/probe-jmap/releases)のノートの先頭に、コピーして使う`uses`の行があります。

```yaml
steps:
  - name: Read the latest message
    uses: github.com/mozership/probe-jmap@f64bf107c6fd7435c4bb3f84e2f9116da761aadf # v0.3.0
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
| `calls` | Array | いいえ | - | メソッドの呼び出し。この順に1回のリクエストで送ります。キーがないステップは[セッションだけを取得](#セッションだけを取得する)します。空のリストはエラーです |
| `using` | Array | いいえ | 推定 | リクエストが使うcapability。書いたとおりに送ります。既定ではcoreと、`calls`のメソッドを定義するcapabilityです |
| `using_also` | Array | いいえ | - | 推定したcapabilityに加えるcapability。ベンダーのものなど。JMAPのキーではなく、`using`とは一緒に書けません |
| `account_id` | String | いいえ | プライマリアカウント | `args`に`accountId`がない呼び出しのアカウント |
| `basic_auth` | Object | いいえ | - | HTTP Basic認証の`username`と`password` |
| `headers` | Object | いいえ | - | `Authorization: Bearer <token>`などのリクエストヘッダー |
| `keep_session` | Boolean | いいえ | `false` | セッションをジョブの中で保持します。保持を指定した後続のステップは、セッションを取得し直さずに呼び出しを送ります。[セッションを保持する](#セッションを保持する)を参照してください |
| `timeout` | Duration | いいえ | `30s` | 2つのリクエストを合わせた制限時間。`10s`のような形式か秒数で指定します。`0`を指定すると制限しません |

`calls`の各呼び出しには次を書きます。

| フィールド | 必須 | 説明 |
|---|---|---|
| `method` | はい | `Email/get`のようなメソッド名 |
| `args` | いいえ | メソッドの引数 |
| `id` | いいえ | 呼び出しのid。back-referenceと`res.results`で使います。既定は`c`と呼び出しの位置（`c0`から）です |

`args`に`accountId`も`#accountId`もない呼び出しは、`account_id`のアカウントで行います。`account_id`もなければ、セッションがメソッドのcapabilityに示すプライマリアカウントで行います。`Core/echo`と`PushSubscription`のメソッドは`accountId`を取らないので、渡しません。

名前が`#`で始まる引数（back-reference）では、`name`を省略できます。省略すると、`resultOf`が指す呼び出しのメソッドを補います。back-referenceは引数全体を置き換えるため、`filter`の中のように引数の内側には書けません。

### capability

`using`はメソッド名の型から推定します。

| 型 | capability |
|---|---|
| `Core`、`PushSubscription` | `urn:ietf:params:jmap:core` |
| `Mailbox`、`Thread`、`Email`、`SearchSnippet` | `urn:ietf:params:jmap:mail` |
| `Identity`、`EmailSubmission` | `urn:ietf:params:jmap:submission` |
| `VacationResponse` | `urn:ietf:params:jmap:vacationresponse` |
| `MDN` | `urn:ietf:params:jmap:mdn` |
| `Blob` | `urn:ietf:params:jmap:blob` |
| `Quota` | `urn:ietf:params:jmap:quota` |
| `SieveScript` | `urn:ietf:params:jmap:sieve` |
| `AddressBook`、`ContactCard` | `urn:ietf:params:jmap:contacts` |

`Blob/copy`は例外です。RFC 8620が定義しているので、coreのcapabilityと推定し、セッションがcoreのプライマリアカウントを示していれば、そのアカウントで行います。

これ以外の型のメソッドには、`using`か`using_also`が必要です。

- `using`は推定したものを置き換え、書いたとおりに送ります。capabilityを外したいときや、サーバーにないcapabilityを送りたいときに使います。
- `using_also`は推定したものの後ろに、重複しないように加えます。表にない型のメソッドは、加えたcapabilityのどれかに属するものとして扱います。そのため、標準の型とベンダーの型を、すべてのcapabilityを並べずに一緒に書けます。

```yaml fragment
with:
  using_also: [urn:example:vendor]
  calls:
  - method: Mailbox/get
  - method: x:Domain/query
```

この例では、`using`としてcore、`urn:ietf:params:jmap:mail`、`urn:example:vendor`を送ります。ベンダーのメソッドはcapabilityがわからないので、`accountId`を渡しません。

1つのステップに両方は書けません。ジョブの`defaults`が`using_also`を指定しているときは、`using`を書くステップで`using_also: null`を指定します。

上記のパラメータ以外のキーを`with`に書くと、何も送る前にステップが失敗します。`action.yml`はパラメータを`params`として宣言しているため、`probe check`がそのキーを行番号とともに報告します。

### セッションだけを取得する

`calls`のないステップは、セッションを取得するだけで、APIには何も送りません。サーバーが起動するのを待つときや、セッションの内容を確かめるときに使います。この例と次の例では、アクションに[`actions`](/ja/reference/yaml-configuration#actions)で`jmap`という名前を付け、`uses`と`defaults`でその名前を使っています。

```yaml fragment
- name: Wait for the server
  uses: jmap
  retry: {max_attempts: 60, interval: 1s}
  test: status == 0 && res.session.capabilities["urn:ietf:params:jmap:mail"] != nil
```

このとき`res`はセッションのリクエストへのレスポンスで、`res.results`と`res.responses`は空です。`status`は、サーバーがセッションを返せば`0`です。`using`、`using_also`、`account_id`は呼び出しのためのものなので、このステップは読みません。そのため、ジョブの`defaults`で指定しておいても構いません。

この動きになるのは、キーを書かなかったときだけです。`calls`が空のリストや値なしのときはステップが失敗します。呼び出しが抜け落ちたリストを、呼び出しを求めないステップと取り違えないためです。

### セッションを保持する

アクションは、ステップごとにセッションを取得します。`keep_session: true`を指定したステップは、取得したセッションをジョブの中で保持します。同じジョブの後続のステップも保持を指定していれば、そのセッションを使って呼び出しを送るので、リクエストが2回から1回になります。ジョブの`defaults`に書けば、すべてのステップに適用されます。

```yaml fragment
jobs:
- name: jmap
  defaults:
    jmap:
      url: https://jmap.example.com
      keep_session: true
```

最初のステップより前には何も取得しません。保持を指定した最初のステップが、`keep_session`がないときと同じようにセッションを取得します。

- セッションは、取得したときのセッションのURLとヘッダー（`basic_auth`を含む）ごとに保持します。そのため、別のユーザーのステップは自分のセッションを取得します。
- `calls`のないステップはセッションそのものを求めるので、必ず取得し、取得したものを保持します。
- APIは、レスポンスごとにセッションの状態を返します。それが保持しているセッションの`state`と違うときや、APIが2xx以外を返したときは、セッションを捨て、次のステップで取得し直します。
- サーバーが返さなかったセッションは保持しません。
- セッションを保持するのはジョブの1回の実行の間だけで、ほかのジョブとは共有しません。

`req.session_kept`は、`res.session`がジョブで保持していたセッションか、そのステップで取得したセッションかを示します。保持していたセッションを使ったときの`rt`は、APIのリクエストだけの時間です。保持していたセッションを使うと、サーバーが受け付けなくなった認証情報は、`session`の種類のエラーではなく、APIのレスポンスとして現れます。

## レスポンスオブジェクト

| プロパティ | 型 | 説明 |
|---|---|---|
| `res.code` | Integer | HTTPステータスコード |
| `res.status` | String | `"200 OK"`のようなHTTPステータス行 |
| `res.headers` | Object | 正規化した名前をキーとするレスポンスヘッダー |
| `res.session` | Object | サーバーがこのステップ、または[保持した](#セッションを保持する)ステップに返したセッションオブジェクト。ないときは`null` |
| `res.results` | Object | 各メソッドのレスポンスの引数。呼び出しのidをキーにします。メソッドのエラーもエラーオブジェクトとしてここに入ります |
| `res.responses` | Array | すべてのメソッドのレスポンス。順に`name`、`args`、`id`を持ちます |
| `res.errors` | Array | 失敗したもの。内容は下記のとおりです。何も失敗しなければ空です |
| `res.body` | Any | レスポンスボディ全体。JSONなら解析した値、それ以外は文字列 |
| `res.rawbody` | String | 解析前のボディ。ボディがJSONのときに入ります |
| `req` | Object | `session_url`、`session_kept`、APIの`url`、`using`、送った`calls`、`headers`。`calls`のないステップでは、`url`は`""`で、`using`と`calls`は空です |
| `rt` | Duration | セッションとAPIのリクエストを合わせた時間。セッションを保持していたときは、APIのリクエストだけの時間 |
| `status` | Integer | APIが2xxでメソッドのレスポンスを返し、`res.errors`が空なら`0`。`calls`のないステップでは、サーバーがセッションを返せば`0`。それ以外は`1` |

JMAPのサーバーは、実行できなかったメソッドにもHTTP 200で応答します。そのため、`res.code`だけでは呼び出しが成功したかわかりません。`res.errors`にはすべての失敗が入ります。各要素は、サーバーが返したエラーオブジェクトに次のフィールドを加えたものです。

| `kind` | 失敗したもの | 加えるフィールド |
|---|---|---|
| `method` | `error`のレスポンスが返ったメソッドの呼び出し | `id`、`method` |
| `notCreated`、`notUpdated`、`notDestroyed` | `/set`の呼び出しのレコード | `id`、`method`、`key`（作成のidかレコードのid） |
| `request` | problem detailsのオブジェクトが返ったリクエスト全体 | - |
| `session` | サーバーが返さなかったセッション | `description` |

サーバーがセッションを返さなかったときは、メソッドの呼び出しを送りません。このとき`res`は、`calls`のないステップと同じく、セッションのリクエストへのレスポンスです。

サーバーが返したレスポンスはすべて結果として扱います。そのため、メソッドのエラーや401もテストで確かめられます。接続の拒否やタイムアウトのように、レスポンスを得られなかったリクエストだけが、ステップをエラーとして失敗させます。

## 使用例

### メッセージを送り、届くのを待つ

`Email/set`でメッセージを作り、`EmailSubmission/set`で送ります。送るメッセージは作成のid `#msg`で指します。続くステップでは`retry`で届くのを待ちます。

```yaml
steps:
  - name: Send a message to bob
    uses: github.com/mozership/probe-jmap@f64bf107c6fd7435c4bb3f84e2f9116da761aadf # v0.3.0
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
    uses: github.com/mozership/probe-jmap@f64bf107c6fd7435c4bb3f84e2f9116da761aadf # v0.3.0
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
    uses: github.com/mozership/probe-jmap@f64bf107c6fd7435c4bb3f84e2f9116da761aadf # v0.3.0
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
