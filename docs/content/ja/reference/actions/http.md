# HTTPアクション

`http`アクションはHTTPリクエストを実行し、レスポンスをアサーションやoutputsから参照できるようにします。

## 基本的な構文

HTTPのステップにはURLとメソッドを指定し、レスポンスを`test`で検証します。

```yaml
steps:
  - name: Check the API
    uses: http
    with:
      url: "https://api.example.com/health"
      method: GET
    test: res.code == 200
```

## パラメータ

以下のフィールドでリクエストを指定します。いずれもテンプレート式を書けます。

| パラメータ | 型 | 必須 | デフォルト | 説明 |
|---|---|---|---|---|
| `url` | String | 必須 | - | リクエストURL。メソッド省略記法でパスを渡す場合はベースURL |
| `method` | String | 必須 | - | HTTPメソッド。メソッド省略記法を使う場合はそちらが設定します |
| `headers` | Object | 任意 | - | リクエストヘッダー |
| `body` | StringまたはObject | 任意 | - | リクエストボディ。`content-type`がJSONの型のとき、オブジェクトはJSONにシリアライズされます。JSONの型とは、`charset`などのパラメータの有無を問わない`application/json`と、`+json`で終わる型です |
| `timeout` | Duration | 任意 | `30s` | レスポンスの読み取りまで含めた、リクエスト全体の制限時間 |
| `basic_auth` | Object | 任意 | - | HTTP Basic認証の`username`と`password`。`Authorization`ヘッダーとして送ります |
| `form` | Object | 任意 | - | フォームのフィールド。`application/x-www-form-urlencoded`のボディとして送ります。[フォームの送信](#フォームの送信)を参照 |
| `multipart` | Object | 任意 | - | フォームのフィールドとファイル。`multipart/form-data`のボディとして送ります。[ファイルのアップロード](#ファイルのアップロード)を参照 |
| `cookies` | Object | 任意 | - | 送るCookie。名前と値で指定します。[Cookie](#cookie)を参照 |
| `trace_header` | BooleanまたはString | 任意 | `false` | リクエストの実行、ジョブ、ステップを示すヘッダーを送ります。`true`なら`X-Probe-Trace`、文字列ならその名前のヘッダーです。[リクエストのトレース](#リクエストのトレース)を参照 |
| `keep_cookies` | Boolean | 任意 | `false` | サーバーが設定したCookieをジョブで保持し、Cookieを保持する以降のステップで送ります。[Cookie](#cookie)を参照 |
| `openapi` | Objectまたは`false` | 任意 | - | `spec`をパスとするOpenAPIドキュメントとリクエストおよびレスポンスを照合し、どちらかに違反があればステップを失敗させます。`request: false`はレスポンスだけを照合し、`strict: true`はドキュメントが宣言していないプロパティも違反にし、`false`はジョブの`defaults`が求める照合を外します。[OpenAPIによる検証](#openapiによる検証)を参照 |

リダイレクトとTLS検証を指定するパラメータはありません。リダイレクトは既定で追跡します。

`headers`で指定しない限り、すべてのリクエストは`Accept: */*`と`User-Agent: probe-http/1.0.0`を送ります。ヘッダー名は大文字と小文字を区別せずに比較するため、`user-agent: my-agent`と書けば既定値を置き換えます。

### `timeout`

`timeout`には`10s`や`1m30s`のようなduration文字列、または秒数の数値を指定します。`0`を指定すると制限しません。負の値、durationに収まらないほど長い値（約292年を超える値）、1ナノ秒に満たない値はエラーになります。

```yaml
  - name: Slow endpoint
    uses: http
    with:
      url: "{{vars.api_url}}/report"
      method: GET
      timeout: 60s
    test: res.code == 200
```

制限時間を超えたリクエストは`Client.Timeout exceeded`のエラーになり、ステップは失敗します。

ジョブの`defaults`でまとめて指定できます。

```yaml
jobs:
  - name: API checks
    defaults:
      http:
        timeout: 5s
    steps:
      - name: Health
        uses: http
        with:
          get: /health
        test: res.code == 200
```

ステップの`timeout`はこれとは別の、アクション実行1回ごとの外側の制限です（既定5分）。`with.timeout`はHTTPリクエストそのものの時間を、ステップの`timeout`はそれを包むアクション呼び出しの時間を制限します。応答を返さないまま固まったアクションを止めるのは後者です。

### メソッド省略記法

`get` `head` `post` `put` `patch` `delete` `connect` `options` `trace`は、メソッドとパスを1つのキーで指定します。値は完全なURLか、`url`からの相対パスです。ジョブの`defaults`と組み合わせると簡潔に書けます。

```yaml
name: API
vars:
  api_url: "{{API_URL}}"
  user_name: alice
jobs:
  - name: API checks
    defaults:
      http:
        url: "{{vars.api_url}}"
        headers:
          accept: application/json
    steps:
      - name: List users
        uses: http
        with:
          get: /users
        test: res.code == 200

      - name: Create a user
        uses: http
        with:
          post: /users
          headers:
            content-type: application/json
          body:
            name: "{{vars.user_name}}"
        test: res.code == 201
```

### フォームの送信

`form`は、HTMLのフォームと同じく、フィールドを`application/x-www-form-urlencoded`のボディとして送ります。フィールドの値には文字列、数値、真偽値を書けます。リストを書くと、その値ごとに同じ名前のフィールドを送ります。

```yaml
  - name: Log in with a form
    uses: http
    with:
      post: /login
      form:
        user: "{{vars.user}}"
        password: "{{vars.password}}"
        scope: [read, write]
    test: res.code == 302
```

フィールドは名前順に並べてパーセントエンコードします。`req.body`には送った内容がそのまま入ります。

### ファイルのアップロード

`multipart`は、テキストのフィールドとファイルを`multipart/form-data`のボディとして送ります。文字列、数値、真偽値で書いたフィールドはテキストのフィールドです。マップで書いたフィールドはファイルで、中身は`file`のパスから読むか、`content`に直接書きます。リストを書くと、その値ごとに同じ名前のフィールドを送るので、1つの名前で複数のファイルを送れます。

```yaml
  - name: Upload an avatar
    uses: http
    with:
      post: /api/images
      multipart:
        title: avatar
        image:
          file: ./fixtures/logo.png
        attachments:
          - file: ./fixtures/terms.pdf
          - content: "a,b\n1,2\n"
            filename: data.csv
            content_type: text/csv
    test: res.code == 201
```

ファイルには次のキーを書けます。

| キー | 説明 |
|---|---|
| `file` | 送るファイルのパス。ほかのアクションのパスと同じく、カレントディレクトリからの相対パスです |
| `content` | `file`の代わりに送る中身 |
| `filename` | ファイルとともに送るファイル名。既定は`file`のベース名、`content`の場合はフィールド名です |
| `content_type` | ファイルのメディアタイプ。既定は`filename`の拡張子から決まるもので、決まらなければ`application/octet-stream`です |

テキストのフィールドを先に、ファイルを後に、それぞれ名前順で送ります。S3へのPOSTアップロードのように、ファイルより前にあるフィールドしか読まないサーバーがあるためです。リストの値は、テキストのフィールド同士、ファイル同士の中で書いた順に送ります。テキストとファイルを混ぜたリストでは、テキストの値が先になります。ファイルはステップを実行するたびに読むので、リトライしたステップはその時点のファイルを送ります。

ボディは長さを付けて送り、表示はしません。`req.body`は空になり、代わりに`req.multipart`に書いた内容が入ります。

`form`と`multipart`はそれぞれ`Content-Type`ヘッダーを設定し、`headers`やジョブの`defaults`で指定したもの（`application/json`など）を置き換えます。どちらも`body`とは同時に指定できず、互いに同時に指定することもできません。

## レスポンスオブジェクト

リクエストの後、`res`に応答の内容が入ります。

| フィールド | 型 | 説明 |
|---|---|---|
| `res.code` | Integer | ステータスコード（例: `200`） |
| `res.status` | String | ステータス行（例: `"200 OK"`） |
| `res.headers` | Object | レスポンスヘッダー。キーは`Content-Type`のような正規形 |
| `res.body` | Any | レスポンスボディ。JSONならオブジェクトや配列に解析され、それ以外は文字列 |
| `res.rawbody` | String | 解析前のボディ。JSONとして解析したときに入ります |
| `res.filepath` | String | バイナリレスポンスを保存したファイルのパス。`Content-Type`がテキストでないものをバイナリとします。テキストは`text/*`、JSON、XML、JavaScript、フォーム、`application/problem+json`や`image/svg+xml`のように`+json`か`+xml`で終わる型です。`Content-Type`のないレスポンスはテキストです |
| `res.cookies` | Object | サーバーが設定したCookie。名前と値で入り、リダイレクトの途中で設定されたものも含みます |
| `res.violations` | Array | リクエストとレスポンスのうちOpenAPIドキュメントが許可しないもの。すべて許可されていれば空です。`openapi`を指定したときだけ入ります |
| `res.contract` | Object | OpenAPIドキュメントでレスポンスを対応付けた先。`spec`、`GET /users/{id}`のような`operation`、`200`、`2XX`、`default`のような`response`が入ります。`response`は、オペレーションがそのステータスのレスポンスを宣言していない場合は入りません。ドキュメントにリクエストのオペレーションがある場合だけ入ります |
| `rt.duration` | String | ラウンドトリップ時間（例: `"120ms"`） |
| `rt.sec` | Float | ラウンドトリップ時間（秒） |
| `status` | Integer | ステータスコードが2xxなら`0`、それ以外は`1` |

## レスポンス例

JSONレスポンスの値は`res.body`から直接読みます。

```yaml
    test: |
      res.code == 200 &&
      res.headers["Content-Type"] contains "application/json" &&
      res.body.status == "ok" &&
      len(res.body.items) > 0
    outputs:
      first_id: res.body.items[0].id
      elapsed_ms: rt.sec * 1000
```

テキストやHTMLのレスポンスでは、`res.body`は文字列そのものです。

```yaml
    test: |
      res.code == 200 &&
      res.body contains "<title>" &&
      len(res.body) > 100
```

## 一般的なパターン

上記のパラメータには、よく使う組み合わせがいくつかあります。トークンをステップ間で受け渡す、エラーレスポンスを検証する、まだ反映されていないリクエストをリトライする、といった形です。

### 認証

トークンはログインのステップの出力として取り出し、後続のステップから参照します。

```yaml
  - name: Log in
    id: auth
    uses: http
    with:
      url: "{{vars.api_url}}/login"
      method: POST
      headers:
        content-type: application/json
      body:
        user: "{{vars.user}}"
        password: "{{vars.password}}"
    test: res.code == 200
    outputs:
      token: res.body.access_token

  - name: Call a protected endpoint
    uses: http
    with:
      url: "{{vars.api_url}}/me"
      method: GET
      headers:
        authorization: "Bearer {{outputs.auth.token}}"
    test: res.code == 200
```

HTTP Basic認証を受け付けるサーバーには、`basic_auth`でユーザー名とパスワードを渡します。`basic_auth`はそこから`Authorization`ヘッダーを組み立てます。ジョブの`defaults`に書けば、ジョブのすべてのリクエストに適用されます。

```yaml
- name: Admin API
  defaults:
    http:
      url: "{{vars.api_url}}"
      basic_auth:
        username: "{{vars.admin_user}}"
        password: "{{vars.admin_password}}"
  steps:
    - name: List users
      uses: http
      with:
        get: /admin/users
      test: res.code == 200 && len(res.body) > 0
```

ユーザー名にはコロンを含められません。そこでユーザー名が終わったと解釈されるためです。パスワードは空でも構いません。送れるのはどちらか一方なので、`basic_auth`と`authorization`ヘッダーは同時に指定できません。パスワードと、そこから組み立てたヘッダーは、ほかの資格情報と同じく出力では伏せられます。

### Cookie

サーバーが設定したCookieは、ブラウザと同じくリダイレクト先にも送ります。そのため、セッションのCookieを設定してリダイレクトするログインでも、リダイレクト先のページまで届きます。設定されたCookieは、リダイレクトの途中で設定されたものも含めて、名前と値で`res.cookies`に入ります。

`keep_cookies: true`を指定すると、サーバーが設定したCookieをジョブで保持し、同じジョブの以降のステップのうち、同じくCookieを保持するステップで送ります。どのCookieをどのURLへ送るかは、ブラウザと同じくドメイン、パス、有効期限、`Secure`属性に従います。サーバーが削除したCookieや期限が切れたCookieは送りません。ジョブの`defaults`に書けば、ジョブのすべてのリクエストに適用されます。

```yaml
- name: Logged-in pages
  defaults:
    http:
      url: "{{vars.api_url}}"
      keep_cookies: true
  steps:
    - name: Log in
      uses: http
      with:
        post: /login
        form:
          user: "{{vars.user}}"
          password: "{{vars.password}}"
      test: res.code == 200 && res.cookies.session != ""

    - name: My page
      uses: http
      with:
        get: /me
      test: res.code == 200
```

Cookieはジョブごとに保持するので、同時に走るジョブ同士で互いのCookieは見えません。繰り返すジョブの各回と、[embedded](/ja/reference/actions/embedded)アクションで実行するジョブは、Cookieを持たない状態から始まります。別のジョブへCookieを渡すには、アウトプットで渡し、渡した先で`cookies`を使って送ります。

`cookies`は、`keep_cookies`の有無にかかわらず、名前と値でCookieを送ります。保持しているCookieに同じ名前のものがあれば、こちらで置き換えます。送り先はリクエストのホストだけで、リダイレクトで別のホストへ移ったときは送りません。`cookie`ヘッダーを書いた場合は、それもあわせて送ります。

```yaml
    - name: Someone else's session
      uses: http
      with:
        get: /me
        cookies:
          session: "{{outputs.other.session}}"
      test: res.code == 200
```

`cookies`と`res.cookies`の値は、ほかの認証情報と同じく出力では隠し、名前は表示します。

### リクエストのトレース

`trace_header: true`を指定すると、各リクエストに送信元を示す`X-Probe-Trace`ヘッダーを付けます。サーバーのアクセスログで、どのリクエストかを見分けられます。

```
X-Probe-Trace: run=7f3a9c21e4b05d68; job=login; step=auth; repeat=0; attempt=1
```

| フィールド | 値 |
|---|---|
| `run` | probeの実行のID。その実行のすべてのリクエストで同じです。JSONレポートの`run_id`と同じ値で、embeddedアクションで実行するジョブも同じ値を使います |
| `job` | ジョブのID |
| `step` | ステップのID。IDのないステップは`step_<番号>`になります |
| `repeat` | 繰り返すジョブの何回目か（0から） |
| `attempt` | リトライするステップの何回目の試行か（1から） |

空白、セミコロン、ASCII以外の文字を含む値は、URLのクエリと同じくエスケープします。ジョブの`defaults`に書けば、ジョブのすべてのリクエストに適用されます。`true`の代わりにヘッダー名を書くと、同じ値をその名前のヘッダーで送ります。サーバーが既にログに出しているヘッダーを使う場合に便利です。

```yaml
- name: Traced checks
  defaults:
    http:
      url: "{{vars.api_url}}"
      trace_header: X-Correlation-Id
  steps:
    - name: List users
      id: users
      uses: http
      with:
        get: /users
      test: res.code == 200
```

このヘッダーは`headers`に書いたヘッダーと同じく送られ、`req.headers`にも表示されます。`headers`に同じ名前のヘッダーを書いた場合はエラーになります。オプション名が`trace`でないのは、`trace`がTRACEメソッドの省略記法だからです。

### OpenAPIによる検証

`openapi`を指定すると、各リクエストとそのレスポンスをOpenAPIドキュメントと照合します。ステップに書いた`test`とは独立に、ドキュメントに合うかどうかでリクエストとレスポンスを判定します。ジョブの`defaults`に書けば、ジョブのすべてのリクエストを照合します。

```yaml
- name: Users
  defaults:
    http:
      url: "{{vars.api_url}}"
      headers:
        content-type: application/json
      openapi:
        spec: ./openapi.yml
  steps:
    - name: Create a user
      uses: http
      with:
        post: /users
        body:
          name: probe
      test: res.body.name == "probe"
    - name: Reject a user without a name
      uses: http
      with:
        post: /users
        body: {}
        openapi:
          request: false
      test: res.code == 400
    - name: Health
      uses: http
      with:
        get: /health
        openapi: false
      test: res.code == 200
```

リクエストとレスポンスは、リクエストのメソッドとパスでオペレーションに対応付けます。パスの先頭からはドキュメントの`servers`のパスを除きます。リクエストはステップが送ったとおりに、`cookies`で送ったCookieや`keep_cookies`で保持したCookieも含めて照合し、次の場合にステップは種類`contract_request`で失敗します。

- パス、クエリ、ヘッダー、Cookieのパラメータが、必須なのにない、またはスキーマに合わない
- ボディが、必須なのにない、`Content-Type`がオペレーションの宣言する型ではない、またはスキーマに合わない
- オペレーションの`security`が求める認証情報がない

レスポンスは、リダイレクトされた場合は最後のリクエストで照合し、次の場合にステップは種類`contract_response`で失敗します。

- メソッドとパスに対応するオペレーションがドキュメントにない
- オペレーションがそのステータスコードも`default`のレスポンスも宣言していない
- レスポンスの`Content-Type`が、そのレスポンスの宣言する型ではない
- レスポンスが宣言するヘッダー、またはボディがスキーマに合わない
- JSONのボディが空、またはスキーマがnullを許可しないのに`null`

ボディはJSON、テキスト、XML、YAML、CSV、フォームとして照合します。画像やPDFなどほかの型のボディは、`Content-Type`だけを照合します。

両方に違反がある場合、種類は`contract_request`です。ワークフローがドキュメントの許可しないものを送っており、それがレスポンスの違反の原因かもしれないからです。`test`が成り立っていても失敗し、`test`は評価しません。`test`のないステップは、リクエストとレスポンスがドキュメントに合っていれば成功します。リクエストとレスポンスはドキュメントとの照合で検証済みだからです。`retry`を指定すれば、`test`が成り立つまで再試行するのと同じく、リクエストとレスポンスがドキュメントに合うまで再試行します。違反はそれぞれ`res.violations`、端末、レポートに`{in, field, reason, message}`の形で入ります。`in`は`request`か`response`で、`field`は`$.id`のようなボディのフィールドです。`field`は該当するときだけ入ります。

拒否されることを確かめるために、ドキュメントの許可しないものをわざと送るステップには`request: false`を書きます。レスポンスは引き続き照合するため、ドキュメントが宣言していない拒否の仕方をすればステップは失敗します。`security`が求める認証情報なしで送るリクエストもこれにあたり、`401`を確かめる場合などに使います。

#### strict

スキーマは通常、宣言していないプロパティも許可します。そのため、たとえば`password_hash`まで返すレスポンスもスキーマに合っていることになります。`strict: true`を指定すると、次の場合にもステップは失敗します。

- JSONのボディに、スキーマが宣言していないプロパティがある。対象は、プロパティか`patternProperties`を書き、`additionalProperties`を書いていないスキーマです
- オペレーションが宣言していないクエリパラメータがある
- リクエストに`readOnly`のプロパティ、レスポンスに`writeOnly`のプロパティがある

```yaml
      openapi:
        spec: ./openapi.yml
        strict: true
```

`additionalProperties`を書いたスキーマには、ほかに何を許可するかが明示されています。`true`やスキーマ（`labels`のようなマップ）はほかのプロパティを許可し、`false`は`strict`がなくても違反になります。`type: object`とだけ書いたような、プロパティを何も宣言していないスキーマは何でも許可します。`allOf`、`oneOf`、`anyOf`のスキーマが宣言するプロパティは、宣言されたものとして扱います。ヘッダーとCookieはstrictに照合しません。`Server`やロードバランサーのCookieなど、プロキシ、サーバー、クライアントが付けるものをドキュメントが宣言することはまれだからです。違反は見つかった場所によって`contract_request`か`contract_response`になり、`field`には`$.owner.email`のようにプロパティが入ります。

`spec`はProbeを実行したディレクトリからのパスです。ドキュメントはリクエストを送る前に読むため、読めない、または解析できないドキュメントを指定したステップは、何も送らずにアクションのエラーとして失敗します。ステップに`openapi: false`を書くと、ドキュメントが扱わないエンドポイントなどで照合を外せます。

各ステップのレスポンスを対応付けたオペレーションとレスポンスは、`res.contract`とJSONレポートに入ります。[`probe coverage`](/ja/reference/cli-reference#coverage)は、そのレポートから、ドキュメントのうちどのステップも検証していないものを示します。

### ガードの下での動作

`--read-only`を指定して実行すると、`GET`、`HEAD`、`OPTIONS`だけを送り、それ以外のメソッドは送る前に拒否します。`--allow-host`を指定して実行すると、許可されていないホストへのURLやリダイレクトを拒否します。ポートのないURLは、スキームの既定のポートとして扱います。拒否されたステップは種類`refused`で失敗します。[`--read-only`](/ja/reference/cli-reference#--read-only)を参照してください。

### エラーレスポンスの検証

ここではエラーが期待される結果です。そのためステータスとエラーのボディを検証します。

```yaml
  - name: Unknown id returns 404
    uses: http
    with:
      url: "{{vars.api_url}}/users/does-not-exist"
      method: GET
    test: res.code == 404 && res.body.error != nil
```

### 不安定なエンドポイントのリトライ

`retry`は、テストが通るか試行回数を使い切るまでステップを繰り返します。

```yaml
  - name: Eventually consistent read
    uses: http
    retry:
      max_attempts: 5
      interval: 2s
    with:
      url: "{{vars.api_url}}/orders/{{outputs.create.order_id}}"
      method: GET
    test: res.code == 200
```

## 関連項目

- **[変数](/ja/reference/actions/variables)** - ステップで使える変数
- **[YAML設定](/ja/reference/yaml-configuration)** - ステップのプロパティ
- **[組み込み関数](/ja/reference/built-in-functions)** - 式で使える関数
