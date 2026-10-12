# YAML設定リファレンス

Probeがワークフローファイルから読み取るキー、式が評価されるコンテキスト、バリデーションの規則をまとめます。

## ワークフローの構造

以下の雛形は、ワークフローファイルに書けるプロパティと、それぞれの位置を示しています。

```yaml
name: string                  # 必須: ワークフロー名
description: string           # 任意: ワークフローの説明
vars:                         # 任意: ワークフロー変数
  key: value
secrets:                      # 任意: 出力で伏せる環境変数
  - NAME
defaults:                     # 任意: すべてのジョブに効く、アクションごとの with の既定値
  http:
    url: string
actions:                      # 任意: 外部アクションに付ける名前
  name: string
jobs:                         # 必須: ジョブのリスト
  - name: string              # 必須: ジョブ名
    id: string                # 任意: ジョブ ID。needs から参照する
    needs: [job-id, ...]      # 任意: 依存するジョブ
    always: boolean           # 任意: 依存先のジョブが失敗しても実行する
    skipif: expression        # 任意: 真ならジョブをスキップ
    fail_fast: boolean        # 任意: 失敗したステップより後のステップをスキップ
    defaults:                 # 任意: アクションごとの with の既定値
      http:
        url: string
    repeat:                   # 任意: ジョブを繰り返す
      count: integer
      interval: duration
    steps:                    # 必須: ステップのリスト
      - name: string          # 任意: ステップ名
        id: string            # 任意: ステップ ID。outputs の公開に必要
        uses: string          # 必須: アクション名、または actions で付けた名前
        with:                 # 任意: アクション引数
          key: value
        test: expression      # 任意: アサーション
        echo: string          # 任意: レポートに出力する文字列
        vars:                 # 任意: ステップ変数
          key: value
        outputs:              # 任意: 後続へ渡す値
          key: expression
        skipif: expression    # 任意: 真ならステップをスキップ
        wait: duration        # 任意: 実行前の待機
        timeout: duration     # 任意: ステップのタイムアウト
        iteration:            # 任意: 要素ごとにステップを繰り返す。
          - key: value        # リスト、またはリストになるテンプレート
        retry:                # 任意: 失敗時のリトライ
          max_attempts: integer
          interval: duration
          initial_delay: duration
```

`jobs`は**リスト**です。ジョブIDをキーにしたマップで書くと読み込みに失敗します。

トップレベルの`env`はありません。環境変数は`vars`を通して読みます。

## トップレベルのプロパティ

ワークフローファイルのルートには、`jobs`のほかに6つのプロパティを置きます。名前、説明、開始時点の値、そのうち表示してはいけない値、ステップが共有する`with`の値、そして外部アクションに付ける名前です。

### `name`

**型:** String（必須）
**説明:** ワークフロー名。レポートの先頭に表示されます。

```yaml
name: "API Health Check"
```

### `description`

**型:** String（任意）
**説明:** ワークフローの説明。

```yaml
description: |
  本番 API を確認します。
  - エンドポイントの死活
  - レスポンスタイム
```

### `vars`

**型:** Object（任意）
**説明:** すべてのジョブとステップから`vars.<name>`で参照できる変数です。

環境変数が見えるのは`vars`の中だけで、変数名をそのまま書いて参照します。値は最初のジョブが始まる前に一度だけ評価されます。

```yaml
vars:
  # 環境変数を読む
  api_url: "{{API_URL}}"

  # デフォルト付き
  timeout: "{{REQUEST_TIMEOUT ?? '30s'}}"

  # 読み込み時に計算する
  run_id: "{{random_str(8)}}"

  # ネストした値も書ける
  auth:
    user: "{{API_USER}}"
```

varは、`probe --var api_url=http://localhost:8080 workflow.yml`のようにワークフローの実行時に与えることもできます。与えた値はここに書いた値の代わりにそのまま使われ、それを読む他のvarもその値を読みます。[`--var`](/ja/reference/cli-reference#--var)と、環境変数をファイルから読む[`--env-file`](/ja/reference/cli-reference#--env-file)を参照してください。

ステップの式から環境変数を直接読むことはできません。式のコンテキストに`env`は存在しないため、`vars`に置いて`vars.<name>`で参照します。

ある変数から別の変数を`vars.<name>`で参照できます。参照先がブロックのどこに書かれていても構いません。各変数は参照している変数の後に評価されるため、ランダムなパスワードのように一度だけ計算される値は、それを使って組み立てたどの変数でも同じになります。

```yaml
vars:
  http_port: "{{HTTP_PORT ?? '18080'}}"
  base: "http://localhost:{{vars.http_port}}"
  password: "{{PASSWORD ?? random_str(24)}}"
  basic_auth: "Basic {{encode_base64('alice:' + vars.password)}}"
```

直接または他の変数を経由して自分自身を参照する変数があると、最初のジョブが始まる前に`vars: circular reference: a -> b -> a`のように経路を示すエラーでワークフローが止まります。変数でない名前は nil として読まれるので、`vars.<name> ?? 'default'`はデフォルト値になります。`vars[KEY]`のように実行時に決まるキーで`vars`を読む変数は、他のすべての変数の後に評価されます。テンプレートファイルから作る変数のように`template`を呼ぶ変数は、テンプレートが何を読むかが実行するまで分からないため、`template`を呼ばないすべての変数の後に評価されます。テンプレートが、`template`を呼ぶ別の変数のうちまだ評価していないものを読む場合は、その変数を先に評価します。両者の参照が循環していれば、ほかの変数の循環と同じくワークフローを止めます。

リストの中のテンプレートも、マップと同じくどの深さでも評価されます。`ports: ["{{vars.http_port}}"]`はポート番号を持ちます。

nil のフィールドを読む`{{nothing.here}}`のように評価できないテンプレートを持つ変数があると、最初のジョブが始まる前に終了ステータス2でワークフローが止まります。エラーには`vars.a: {{nothing.here}}: cannot fetch here from <nil>`のように変数とテンプレートが示されます。評価できなかった変数を参照する変数は評価しないため、同じエラーが重ねて出ることはありません。

### `secrets`

**型:** 文字列の配列（任意）
**説明:** Probeが表示・書き出すものに、値を出してはいけない環境変数の名前です。

```yaml
secrets:
  - API_TOKEN
  - DB_PASSWORD
vars:
  api_token: "{{API_TOKEN}}"
  db_password: "{{DB_PASSWORD}}"
```

列挙した変数の値が現れる箇所には、代わりに`<secret:NAME>`と書きます。対象は、端末のレポート、ステップ名、`echo`、失敗したステップについて表示するリクエストとレスポンス、`--verbose`の出力、アクション自身のログ、`--report`で書き出すファイルです。エラーメッセージで引用符に囲まれたときやJSONに埋め込まれたときのエスケープされた形も伏せます。ステップ、テスト、outputsは本来の値をそのまま使い、変わるのは表示だけです。

設定されていない名前や空文字列の名前は、伏せる値が無いので無視します。短い値は現れるたびに置き換えるため、`1`のような値を指定すると出力中のすべての`1`が伏せられます。

`secrets`とは別に、`Authorization`、`Proxy-Authorization`、`Cookie`、`Set-Cookie`ヘッダの値は常に`<redacted>`と表示します。ログインのステップが返し、後のリクエストで送るトークンのように、実行中に得た値は`secrets`に書けませんが、これらのヘッダのどれかで送られます。Probeはアクションがこれらの値を送る直前と受け取った時点で値を覚え、以降はアクション自身のログも含めて伏せます。

アクションに渡す資格情報も同じです。`with`のどの階層にあっても`password`、`key_passphrase`、`secret_access_key`、`session_token`の値は`<redacted>`と表示します。`dsn`や`url`に書いた`redis://user:password@host`のようなURLは、パスワードの部分だけを伏せ、それ以外はそのまま表示します。そのため、ステップに直接書いたパスワードは、`secrets`に書かなくても`--verbose`の出力やアクションのログに出ません。こうした値のテンプレートを評価できなかった場合、エラーには`with.password`のように値の場所を示しますが、理由は示しません。テンプレートにもエラーの文面にも資格情報が含まれうるためです。`url`がこの扱いになるのは、`redis://app:{{vars.pw}}@host`のようにテンプレートがユーザー情報の部分にある場合だけです。`url`のそれ以外の場所にあるテンプレートのエラーは表示します。

### `defaults`

**型:** Object（任意）  
**説明:** すべてのジョブのステップに効く`with`の既定値です。ジョブの[`defaults`](#defaults-1)と同じく、アクションをキーにして書きます。

ベースURLやDSNのようにどのジョブも使う接続先は、ジョブごとではなくここに一度だけ書きます：

```yaml
defaults:
  db:
    dsn: "postgres://{{vars.user}}@{{vars.host}}/app"
  http:
    url: "{{vars.api_url}}"
    headers:
      accept: application/json

jobs:
  - name: Rows
    steps:
      - name: Count users
        uses: db
        with:
          query: SELECT count(*) AS n FROM users
        test: res.rows[0].n > 0

  - name: API
    defaults:
      http:
        headers:
          authorization: "Bearer {{vars.token}}"
    steps:
      - name: List users          # 既定の url に、両方のヘッダーを付けて送る
        uses: http
        with:
          get: /users
        test: res.code == 200
```

もっとも近くに書いた値が優先されます。ステップが`with`に書いた値、そのジョブの`defaults`、ワークフローの`defaults`の順です。オブジェクトはキーごとにマージされるので、上のステップはワークフローの`accept`ヘッダーとジョブの`authorization`ヘッダーの両方を送ります。既定値の中のテンプレートは、ステップの実行時に、そのステップの`vars`と`outputs`で評価されます。

ワークフローの`defaults`は、[embedded](/ja/reference/actions/embedded)アクションで実行するジョブファイルには届きません。ジョブファイルは独立したファイルで、自身の`defaults`を持ちます。

### `actions`

**型:** 文字列のオブジェクト（任意）  
**説明:** ワークフローが使う[外部アクション](/ja/guide/concepts/actions#外部アクション)に付ける名前です。ステップの`uses`と、ジョブやワークフローの`defaults`のキーに、リポジトリとコミットの代わりにこの名前を書けます。

```yaml
actions:
  graphql: github.com/mozership/probe-graphql@41e4ffa222db58c63c7169117c919e6d252bdbf9 # v0.2.0

jobs:
  - name: Countries API
    defaults:
      graphql:
        url: https://countries.trevorblades.com/graphql
    steps:
      - name: Look up Japan
        uses: graphql
        with:
          query: '{ country(code: "JP") { capital } }'
        test: res.data.country.capital == "Tokyo"
```

アクションを固定するコミットを書く場所が1か所になるので、新しいリリースへの更新は1行の変更で済みます。

- 名前に使えるのは英数字、`_`、`-`で、先頭は英数字です。
- `http`や`shell`のようなProbeのアクションの名前は付けられません。`uses: http`が何を実行するかは、ワークフローによって変わらないようにするためです。そのような名前を付けたワークフローは読み込みに失敗します。
- 値は`uses`と同じ形式で外部アクションを指定します。40文字のコミットSHAを付けた`github.com/<owner>/<repo>[/<dir>]@<commit>`か、`./`、`../`、`/`で始まるローカルのパスです。テンプレートは書けず、別の名前を指すこともできません。
- ステップでアクションを完全な形で書くこともでき、名前で書いた`defaults`はそのステップにも適用されます。1つの`defaults`に、名前とそれが指すアクションの両方を書くことはできません。

Probeはワークフローを読み込む時点で名前をアクションに置き換えるので、それ以降の処理はすべて完全な形のアクションを扱います。特に`--allow-action`には、名前ではなくアクションを指定します。Probeを実行する人が許可した名前が何を指すかを、ワークフローが決められないようにするためです。

[embedded](/ja/reference/actions/embedded)アクションで実行するジョブファイルは、埋め込む側のワークフローの名前で読まれるので、そのステップと`defaults`でも名前を使えます。[名前を付けた外部アクション](/ja/reference/actions/embedded#名前を付けた外部アクション)を参照してください。

## ジョブ

`jobs`はジョブ名からジョブ定義へのマップです。ジョブは順に実行するステップをまとめ、依存先を宣言します。

### ジョブのプロパティ

ジョブの定義では以下のプロパティを指定します。

| プロパティ | 型 | 必須 | 説明 |
|---|---|---|---|
| `name` | String | 必須 | ジョブ名。テンプレート式が使えます |
| `id` | String | 任意 | 他ジョブの`needs`から参照する識別子。省略時は自動採番されます |
| `needs` | Array | 任意 | 先に完了している必要があるジョブのID |
| `always` | Boolean | 任意 | `needs`のジョブが失敗していても、それらが終わり次第実行する |
| `steps` | Array | 必須 | 実行するステップ |
| `skipif` | Expression | 任意 | 真ならジョブ全体をスキップ |
| `fail_fast` | Boolean | 任意 | 失敗したステップより後のステップをスキップ |
| `defaults` | Object | 任意 | アクション名をキーとした`with`の既定値 |
| `repeat` | Object | 任意 | ジョブを繰り返す |

ジョブに`if`、`continue_on_error`、`timeout`はありません。失敗の後にも実行したいジョブには[`always`](#always)を付けます。

#### `needs`

依存のないジョブは並列に開始します。`needs`が参照するのはジョブの**ID**なので、依存される側には明示的な`id`が必要です。

```yaml
jobs:
  - id: setup
    name: Setup
    steps:
      - name: Prepare
        uses: hello
        echo: "ready"

  - name: Test
    needs: [setup]
    steps:
      - name: Run
        uses: hello
        echo: "testing"
```

#### `always`

ジョブは、`needs`に書いたジョブが失敗したとき、または同じ理由でスキップされたときにスキップされます。`always: true`を付けたジョブは、`needs`のジョブがどう終わったかにかかわらず、すべてが終わり次第実行されます。他のジョブが作ったものを片付けるジョブに必要な動作です。これがないと、失敗した実行はテスト用のデータベースやファイルを残し、次の実行がそれにぶつかります。

```yaml
jobs:
  - id: setup
    name: Setup
    steps:
      - name: Create the test database
        uses: db
        with:
          dsn: "{{vars.admin_dsn}}"
          query: CREATE DATABASE probe_test
        test: res.code == 0

  - id: tests
    name: Tests
    needs: [setup]
    steps:
      - name: Count users
        uses: db
        with:
          dsn: "{{vars.test_dsn}}"
          query: SELECT count(*) AS n FROM users
        test: res.rows[0].n > 0

  - name: Cleanup
    needs: [setup, tests]
    always: true
    steps:
      - name: Drop the test database
        uses: db
        with:
          dsn: "{{vars.admin_dsn}}"
          query: DROP DATABASE IF EXISTS probe_test
        test: res.code == 0
```

- このジョブも`needs`のすべてのジョブを待ちます。繰り返すジョブは全回の実行を待ちます。片付ける対象を作るジョブはすべて`needs`に書き、それらと並行して走らないようにします。
- 実行全体の成否は変わりません。`always`が変えるのはどのジョブが走るかであり、終了ステータスではありません。
- 失敗またはスキップされたジョブが公開しなかったoutputはnilとして読まれます。`DROP DATABASE IF EXISTS`のように、それがなくても動くステップを書きます。
- `always`を付けたジョブを`needs`に書いたジョブは、他のジョブに対してと同じく、そのジョブの結果に従います。
- `skipif`は引き続き有効です。両方を書いたジョブは、式が真ならスキップされます。

#### `skipif`

ブール式です。真と評価されるとジョブをスキップします。

```yaml
jobs:
  - name: Production only
    skipif: vars.environment != "production"
    steps:
      - name: Check
        uses: http
        with:
          url: "{{vars.api_url}}/health"
          method: GET
```

#### `fail_fast`

ジョブのステップは順番に実行され、前のステップが失敗していても次のステップは実行されます。これは、それぞれが別のことを確かめていて、レポートに1行ずつ残す価値があるステップに向いています。一方、後のステップが前のステップの作ったものを使う場合、後のステップは前の失敗が原因で失敗し、本当に見るべき1つの失敗が、そこから派生した失敗に埋もれます。`fail_fast: true`を付けると、失敗したステップより後のステップは実行されず、スキップとして報告されます：

```yaml
jobs:
  - name: Orders
    fail_fast: true
    steps:
      - name: Create the table
        uses: db
        with:
          query: CREATE TABLE orders (id INTEGER PRIMARY KEY, total INTEGER)
        test: res.code == 0

      - name: Insert an order          # テーブルを作れなかったときはスキップ
        uses: db
        with:
          query: INSERT INTO orders (total) VALUES (?)
          params: [100]
        test: res.rows_affected == 1
```

- ステップが失敗するのは、`test`が偽のとき、アクションがエラーを返したとき、レスポンスが契約に違反したときです。`test`がなく何にも検査されないステップは失敗しないので、何も止めません。
- スキップされたステップは`wait`を待たず、`outputs`も公開しません。
- [`iteration`](#iteration)の残りのエントリも、どれかが失敗した時点でスキップされます。
- [`repeat`](#repeat)を付けたジョブは、実行のたびにやり直します。失敗がスキップするのは、その回の残りだけです。ステップが実行されなかった回は、そのステップの成功にも失敗にも数えず、`1/3 success (33.3%), 2 skipped`のように別に示します。
- 他のジョブには影響しません。失敗したジョブ一般と同じく、[`needs`](#needs)を通した影響だけがあります。後片付けのように失敗の後にも実行したいステップは、[`always`](#always)を付けたジョブに書きます。

#### `defaults`

ジョブ内の該当アクションを使うステップの`with`にマージされる既定値です。ステップ側の指定が優先され、ここでの指定は、すべてのジョブが共有する[ワークフローの`defaults`](#defaults)より優先されます。キーはステップが`uses`に書くものです。組み込みアクションの名前、完全な形の外部アクション、[`actions`](#actions)で付けた名前のどれかです。

```yaml
jobs:
  - name: API checks
    defaults:
      http:
        url: "{{vars.api_url}}"
        headers:
          authorization: "Bearer {{vars.token}}"
          accept: application/json
    steps:
      - name: Health
        uses: http
        with:
          get: /health        # 既定の url からの相対パス
        test: res.code == 200
```

`http`アクションはメソッドの指定が必須です。`method`を明示するか、`get` / `post` / `put` / `delete` / `patch`といった省略記法を使います。省略記法の値は完全なURLか、`url`からの相対パスです。

```yaml
      - name: Explicit method
        uses: http
        with:
          url: "{{vars.api_url}}/health"
          method: GET
```

#### `repeat`

`repeat`はジョブ全体を一定の間隔で繰り返します。テストを監視に変えるのはこの指定です。

| プロパティ | 型 | 必須 | 説明 |
|---|---|---|---|
| `count` | Integer | 必須 | 実行回数。上限は`PROBE_MAX_REPEAT_COUNT`（既定10000） |
| `interval` | Duration | 任意 | 実行間隔 |
| `async` | Boolean | 任意 | 繰り返しを並行実行する |

```yaml
jobs:
  - name: Poll until ready
    repeat:
      count: 10
      interval: 5s
    steps:
      - name: Check
        uses: http
        with:
          url: "{{vars.api_url}}/status"
          method: GET
        test: res.code == 200
```

各回のステップは、同じ回の前のステップが公開した出力を読みます。`async`で複数の回が同時に動く場合も同じです。後続のジョブは、それぞれの出力について、最後にそれを公開した回の値を読みます。そのため、最後の回が飛ばしたステップの出力は、その前の回の値になります。

## ステップ

ステップは1回のアクション呼び出しと、その周辺の指定から成ります。実行するかを決める条件、成功したかを決めるテスト、後続のステップに残す出力です。

### ステップのプロパティ

ステップの定義では以下のプロパティを指定します。

| プロパティ | 型 | 必須 | 説明 |
|---|---|---|---|
| `uses` | String | 必須 | 実行するアクション。`http`、`shell`などの組み込みアクションか、[外部アクション](/ja/guide/concepts/actions#外部アクション) |
| `name` | String | 任意 | ステップ名 |
| `id` | String | 任意 | このステップの`outputs`の名前空間になる識別子 |
| `with` | Object | 任意 | アクション引数 |
| `test` | Expression | 任意 | アサーション。偽ならステップは失敗します |
| `echo` | String | 任意 | レポートに出力する文字列 |
| `vars` | Object | 任意 | ステップ内だけの変数 |
| `outputs` | Object | 任意 | 後続のステップやジョブへ渡す値 |
| `skipif` | Expression | 任意 | 真ならステップをスキップ |
| `wait` | Duration | 任意 | 実行前の待機時間 |
| `timeout` | Duration | 任意 | ステップのタイムアウト（既定5m） |
| `iteration` | Array またはテンプレート | 任意 | 要素ごとにステップを繰り返す。値は`vars`から参照します |
| `retry` | Object | 任意 | 失敗時のリトライ |

キーは`action`ではなく`uses`、条件は`if`ではなく`skipif`です。

#### `outputs`

後続のステップやジョブへ渡す値です。**`id`を持つステップだけがoutputsを公開します。** `id`が無いと`outputs`ブロックは捨てられます。

値はテンプレートではなく式なので、波カッコは書きません。

```yaml
steps:
  - name: Log in
    id: auth
    uses: http
    with:
      url: "{{vars.api_url}}/login"
      method: POST
    test: res.code == 200
    outputs:
      token: res.body.access_token
      user_id: res.body.user.id
```

後続からはステップIDで名前空間を指定するか、出力名だけで参照します。

```yaml
      headers:
        Authorization: "Bearer {{outputs.auth.token}}"
        X-User: "{{outputs.user_id}}"
```

ハイフンを含むIDは式の識別子として解釈できないため、ブラケットで参照します。

```yaml
    echo: "{{outputs['create-user'].user_id}}"
```

#### `retry`

`retry`は、テストが通るか試行回数を使い切るまで、そのステップだけを繰り返します。

| プロパティ | 型 | 必須 | 説明 |
|---|---|---|---|
| `max_attempts` | Integer | 必須 | 試行回数。1以上で、上限は`PROBE_MAX_ATTEMPTS`（既定10000） |
| `interval` | Duration | 任意 | 試行間隔 |
| `initial_delay` | Duration | 任意 | 最初の試行前の待機 |

```yaml
steps:
  - name: Flaky endpoint
    uses: http
    with:
      url: "{{vars.api_url}}/slow"
      method: GET
    test: res.code == 200
    retry:
      max_attempts: 3
      interval: 2s
```

#### `iteration`

要素ごとにステップを繰り返します。各要素のキーは`vars`から参照できます。

```yaml
steps:
  - name: Check {{vars.path}}
    uses: http
    iteration:
      - path: /health
      - path: /metrics
      - path: /version
    with:
      url: "{{vars.api_url}}{{vars.path}}"
      method: GET
    test: res.code == 200
```

`iteration`には、ワークフローに書いたリストの代わりに、オブジェクトのリストになるテンプレートを1つ書けます。テンプレートはステップに到達した時点で、ワークフローとステップの`vars`、それまでに公開された`outputs`を使って評価されます。そのため、ファイルに置いたケースや、前のステップが見つけたものを要素にできます：

```yaml
steps:
  # ファイルのケースごとに1回実行。ケースはワークフローを変えずに増やせる
  - name: "{{vars.name}} can sign in"
    uses: http
    iteration: "{{ parse_json(file('testdata/users.json')) }}"
    with:
      post: /login
      body:
        user: "{{vars.name}}"
        password: "{{vars.password}}"
    test: res.code == 200

  - name: List orders
    id: orders
    uses: db
    with:
      query: SELECT id, total FROM orders WHERE status = 'open'
    test: res.code == 0
    outputs:
      rows: res.rows

  # 上のステップが読んだ行ごとに1回実行
  - name: "Order {{vars.id}} has a total"
    uses: http
    iteration: "{{ outputs.orders.rows }}"
    with:
      get: "/orders/{{vars.id}}"
    test: res.body.total == vars.total
```

- 値は`{{ }}`のテンプレート1つだけです。前後に文字があるもの、テンプレートが2つあるものは文字列になってしまうため、ワークフローの読み込みに失敗します。
- 各要素はオブジェクトで、そのキーをステップが`vars`として読みます。文字列のリストなど、それ以外のリストはステップを失敗させます。`"{{ map(vars.paths, ({path: #})) }}"`のように`map`でオブジェクトに変換してください。
- 評価できないテンプレート、リストにならないテンプレートは、ステップを実行せずに失敗させます。
- 要素のないリストはステップをスキップし、スキップとして報告します。実行に使う値がないためです。空のリスト`[]`として書いた`iteration`は、`iteration`のないステップと同じく1回実行します。
- 要素はそのまま使われます。要素の値に書いた`{{ }}`は評価されません。ステップ自身の`vars`は、テンプレートのために評価され、さらに要素ごとにも評価されます。ワークフローに書いたリストの要素ごとに評価されるのと同じです。`random_str`を呼ぶvarは、そのたびに別の値になります。
- `iteration`が受け取れる要素は1,048,576個までです。それより長いリストはステップを失敗させます。
- [`repeat`](#repeat)を付けたジョブでは、テンプレートは回ごとに評価され、回によって要素数が変わることがあります。各要素はリスト内の位置ごとに集計され、後続のステップはそれぞれ自身として集計されます。リストに要素がなかった回は、[`fail_fast`](#fail_fast)のジョブで実行されなかった回と同じく、そのステップの成功にも失敗にも数えません。

## 式のコンテキスト

ステップの式から見えるのは次の名前だけです。

| 名前 | 型 | 説明 |
|---|---|---|
| `vars` | Object | ワークフローの変数にステップの`vars`をマージしたもの |
| `res` | Object | アクションのレスポンス |
| `req` | Object | 送信したリクエスト |
| `rt` | Object | レスポンスタイム。`rt.duration`（文字列）と`rt.sec`（秒、浮動小数点数） |
| `status` | Integer | アクションの終了ステータス。成功は`0` |
| `outputs` | Object | 先行ステップが公開した値 |
| `repeat_index` | Integer | ジョブ繰り返し時の現在のインデックス |

このコンテキストに`env`、`jobs`、`steps`はありません。

### `http`の`res`

`res`の内容はアクションごとに異なります。`http`では以下が入ります。

| フィールド | 型 | 説明 |
|---|---|---|
| `res.code` | Integer | ステータスコード（例: `200`） |
| `res.status` | String | ステータス行（例: `"200 OK"`） |
| `res.headers` | Object | レスポンスヘッダー。キーは`Content-Type`のような正規形 |
| `res.body` | Any | レスポンスボディ。JSONならオブジェクトや配列に解析され、それ以外は文字列 |
| `res.rawbody` | String | 解析前のボディ。JSONとして解析したときに入ります |

```yaml
    test: |
      res.code == 200 &&
      res.headers["Content-Type"] contains "application/json" &&
      res.body.status == "ok" &&
      rt.sec < 1
```

他のアクションの`res`は[アクションリファレンス](/ja/reference/actions/variables)を参照してください。

## データ型

ワークフローファイルの値のうち、独自の書き方を持つものが2つあります。時間の長さと、コンテキストを参照する式やテンプレートです。

### Duration

Goのduration文字列か、秒数の数値で指定します。

```yaml
timeout: "30s"
timeout: "5m"
interval: 10        # 10 秒
```

### 式とテンプレート

**テンプレート式**は文字列の中に書き、評価結果で置き換わります。

```yaml
url: "{{vars.api_url}}/users/{{outputs.auth.user_id}}"
```

**ブール式**は波カッコなしでそのまま書きます。

```yaml
test: res.code == 200 && rt.sec < 2
skipif: vars.environment == "local"
```

`outputs`の値も式なので、波カッコは書きません。

評価できないテンプレートはエラーになり、エラーの文面が値に入ることはありません。定義されていない名前は nil として読まれエラーになりませんが、nil のフィールドを読むことや、構文として解釈できない式はエラーです。

| テンプレートの場所 | 起きること |
|---|---|
| ステップの`with`、`vars`、`name` | ステップは種類`template`で失敗し、アクションは実行されません。エラーには`with.headers.authorization`のように値の場所が示されます |
| ワークフローの`vars` | 最初のジョブの前に終了ステータス2でワークフローが止まります |
| ジョブの`name` | ジョブが失敗し、終了ステータスは2です |
| ステップの`echo` | 本文の代わりにエラーを表示し、ステップの結果は変わりません |

```yaml
# 先行するステップが outputs.login を出力していなければ、ステップは失敗する
headers:
  authorization: "Bearer {{outputs.login.token}}"

# 代わりの値を使う
headers:
  authorization: "Bearer {{outputs.login?.token ?? 'none'}}"
```

式の中で使える関数は[組み込み関数](/ja/reference/built-in-functions)を参照してください。

## バリデーション規則

- ワークフローには`name`が必要で、`jobs`は空でないリストである必要があります
- 各ジョブには`name`と1つ以上のステップが必要です
- 各ステップには`uses`が必要で、アクションを指している必要があります。Probeのアクション、外部アクション、`actions`で付けた名前のいずれかです。どれでもない場合は、スキップされるステップであっても、最初のジョブの前に終了ステータス2で実行が止まります
- `actions`の名前はProbeのアクションの名前であってはならず、コミットで固定した外部アクションかローカルのパスを指す必要があります
- `needs`は存在するジョブIDを指し、依存関係は循環していない必要があります
- `repeat.count`は0以上、`retry.max_attempts`は1以上である必要があります
- ステップに`id`が無いと`outputs`は公開されません

`probe check`は、受け付けないキー、解析できない式、公開される前に読む出力とあわせて、これらをワークフローを実行せずに報告します。[CLIリファレンス](/ja/reference/cli-reference#check)を参照してください。

## ファイルのマージ

複数のファイルをカンマ区切りで渡すと順に連結されます。複数回定義されたトップレベルのキーは、最後のファイルの値になります。

```bash
probe base.yml,production.yml
```

マージの規則は[ファイルマージ](/ja/guide/concepts/file-merging)を参照してください。

## 関連項目

- **[CLI](/ja/reference/cli-reference)** - コマンドラインオプション
- **[組み込み関数](/ja/reference/built-in-functions)** - 式で使える関数
- **[環境変数](/ja/reference/environment-variables)** - Probeが読む環境変数
