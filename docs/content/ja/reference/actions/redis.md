# Redisアクション

Redisアクションは、RedisまたはValkeyのサーバーでコマンドを実行し、その応答を返します。APIがキャッシュやセッションストアに残したものを、ワークフローから確かめられます。

[外部アクション](/ja/guide/concepts/actions#外部アクション)として[mozership/probe-redis](https://github.com/mozership/probe-redis)で公開しています。ワークフローが初めて使うときにProbeがダウンロードし、固定したコミットの`action.yml`が示すSHA-256と一致する実行ファイルだけを実行します。Probe v1.21.0以降が必要です。

## 基本的な構文

ワークフローでは40文字のコミットSHAでアクションを固定します。各[リリース](https://github.com/mozership/probe-redis/releases)のノートの先頭に、コピーして使うアクションとコミットがあります。このページの例では、[`actions`](/ja/reference/yaml-configuration#actions)で一度だけ名前を付けています。`actions`にはProbe v1.24.0以降が必要です。それより前のProbeでは、各`uses`にアクションを完全な形で書きます。

```yaml
name: Session
actions:
  redis: github.com/mozership/probe-redis@7d60e0699a914e3c987ed5f2403ed8a7f3d176fa # v0.1.0
jobs:
  - name: session
    steps:
      - name: Sign in
        id: login
        uses: http
        with:
          url: https://api.example.com
          post: /login
          body: {user: ada, password: "{{vars.password}}"}
        test: res.code == 200
        outputs:
          session: res.body.session_id

      - name: The session is in the store, and expires
        uses: redis
        with:
          url: redis://cache.example.com:6379/0
          password: "{{vars.redis_password}}"
          commands:
            - [HGET, "session:{{outputs.login.session}}", user]
            - [TTL, "session:{{outputs.login.session}}"]
        test: status == 0 && res.results[0] == "ada" && res.results[1] > 0
```

ステップごとに新しい接続を開き、`commands`を順に実行して閉じます。そのため、接続に属する状態は次のステップに引き継がれません。トランザクションや、`HELLO 3`とそれに続くコマンドは、1つのステップにまとめて書きます。

## パラメータ

| パラメータ | 型 | 必須 | デフォルト | 説明 |
|-----------|------|----------|---------|-------------|
| `url` | String | Yes | - | サーバー。`redis://[username:password@]host[:port][/db]`の形式です。`rediss`はTLSで接続し、`valkey`と`valkeys`はこの2つの別名です。ポートのデフォルトは`6379`で、`db`は選択するデータベースの番号です |
| `username` | String | No | - | 認証するユーザー。URLにユーザーがないときに使います |
| `password` | String | No | - | パスワード。URLにパスワードがないときに使います |
| `commands` | List | Yes | - | 実行するコマンド。順に実行します。[コマンド](#コマンド)を参照してください |
| `timeout` | Duration | No | `30s` | 接続から最後の応答までの、ステップ全体の制限時間。`10s`または秒数で指定します。`0`で制限なしになります |
| `insecure_skip_tls` | Boolean | No | `false` | `rediss`のサーバーの証明書を検証せずに受け入れます。自分で管理しているサーバーにだけ使ってください |

これら以外のキーを`with`に書くと、何も送らずにステップが失敗します。`action.yml`がこれらを`params`として宣言しているので、`probe check`はそのようなキーを行番号つきで報告します。

URLまたは`with`にユーザー名かパスワードがあれば、アクションはコマンドの前に`AUTH`で認証し、URLがデータベースを指定していればそれを選択します。Probeは、ステップを表示するすべての箇所で`password`の値を伏せます。v1.24.0以降は`url`の中のパスワードも伏せます。

アクションが接続するのは、URLが指す1台のサーバーだけです。クラスタの`MOVED`と`ASK`のリダイレクトは追わず、センチネルにマスターを問い合わせることもしません。そのようなリダイレクトは、ほかと同じエラー応答として扱います。

## コマンド

`commands`の各項目が1つのコマンドで、2つの書き方があります。

| 書き方 | 例 | 読み方 |
|------|---------|----------------|
| リスト | `[SET, greeting, hello world]` | 各項目がコマンドの1語になります。文字列はそのまま、数値はその数字の並びとして送ります。`true`、`false`、`null`は、Redisにそのような値がないため拒否します。文字列として送るには引用符で囲みます |
| 文字列 | `SET greeting "hello world"` | `redis-cli`と同じように空白で語に分けます。二重引用符の中では空白を保ち、`\n`や`\"`などのエスケープを解釈します。一重引用符の中では`\'`以外をそのまま保ちます |

リストの書き方は引用符が要らないので、テンプレートから来る値にはこちらを使います。

次のコマンドは実行しません。

- `AUTH`と、`AUTH`を付けた`HELLO`。ユーザー名とパスワードは、コマンドと一緒に表示されないように、`url`か`username`と`password`で指定します。
- `SUBSCRIBE`、`PSUBSCRIBE`、`SSUBSCRIBE`、`MONITOR`、`SYNC`、`PSYNC`、`CLIENT REPLY`。アクションはコマンドごとに1つの応答を読みますが、これらはサーバーが1つの応答で答えないためです。

## レスポンスオブジェクト

| プロパティ | 型 | 説明 |
|----------|------|-------------|
| `res.results` | Array | 各コマンドへの応答。`commands`と同じ順です |
| `res.errors` | Array | サーバーが返したエラー。なければ空です |
| `req` | Object | パスワードを除きポートを補った`url`と、実際に送った語のリストにした各`commands` |
| `rt` | Duration | 接続から最後の応答までの時間 |
| `status` | Integer | サーバーがどのコマンドにもエラーを返さなければ`0`、そうでなければ`1` |

応答は次のような値になります。

| 応答 | 値 |
|-------|-------|
| `GET`の結果のような文字列、または`OK`のようなステータス | String |
| `INCR`や`TTL`の結果のような整数 | Number |
| 存在しないキーの`GET`のように、値がないもの | `null` |
| `LRANGE`や`MGET`の結果のようなリスト、またはセット | Array。各項目はこの表の値です |
| `HELLO 3`の後にサーバーが送るマップ、倍精度数、真偽値、巨大な数 | Object、Number、Boolean、数字の並びの文字列 |
| エラー | `null`と、`res.errors`の1項目 |

UTF-8でない文字列は、`{"base64": "//4="}`のように、バイト列をbase64で持つオブジェクトになります。

`HELLO 3`がなければサーバーはRESP2で答えます。RESP2ではマップがキーと値を交互に並べたリストになるので、`name`と`age`を持つハッシュの`HGETALL`は`["name", "Ada", "age", "36"]`です。同じステップで先に`HELLO 3`を送ると、`{"name": "Ada", "age": "36"}`として受け取れます。

```yaml
steps:
  - name: A hash as an object
    uses: redis
    with:
      url: redis://localhost:6379
      commands:
        - HELLO 3
        - [HGETALL, "user:1"]
    test: res.results[1].name == "Ada"
```

`res.errors`の各項目には次のプロパティがあります。

| プロパティ | 型 | 説明 |
|----------|------|-------------|
| `index` | Integer | サーバーがエラーを返した`commands`の項目の番号。`0`から数えます。アクション自身が送る`AUTH`と`SELECT`では`-1`です |
| `command` | String | 大文字にしたコマンドの名前。`LPUSH`や`OBJECT ENCODING`など |
| `code` | String | エラーの最初の語で、種類を表します。`ERR`、`WRONGTYPE`、`NOAUTH`、`WRONGPASS`、`NOPERM`、`MOVED`など |
| `message` | String | サーバーが送ったエラーの全文 |

サーバーに接続できた後は、サーバーが返したものはすべて結果なので、テストでエラーを確かめられます。エラーがあってもステップは止まりません。後続のコマンドも実行し、その応答は`res.results`のそれぞれの位置に入ります。

```yaml
steps:
  - name: The key is not a list
    uses: redis
    with:
      url: redis://localhost:6379
      commands:
        - [SET, greeting, hello]
        - [LPUSH, greeting, x]
        - [GET, greeting]
    test: |
      status == 1 &&
      res.results == ["OK", nil, "hello"] &&
      res.errors[0].index == 1 && res.errors[0].code == "WRONGTYPE"
```

アクション自身の`AUTH`や`SELECT`をサーバーが拒否した場合、コマンドは送りません。`res.results`は空で、`res.errors`には番号が`-1`のエラーが1つ入ります。

エラーとして失敗するのは、サーバーと通信できなかったステップだけです。接続の拒否、信頼できない証明書、サーバーによる切断、アクションが読める大きさを超えた応答（16 MiBの文字列、または1,048,576項目のリスト）、タイムアウトがこれにあたります。

## ガードの下での動作

このアクションは実行のガードに従い、`action.yml`で`guard: [read-only, allow-host]`を宣言しています。そのため、どちらの下でも`--allow-action`なしで実行されます。アクションが拒否したステップは、接続する前に種別`refused`で失敗します。

- `--read-only`の下では、すべてのコマンドが下にある読み取り専用のコマンドの一覧に含まれるステップだけを実行します。そうでなければステップ全体を拒否し、拒否のメッセージにコマンドを示します。
- `--allow-host`の下では、`url`のホストとポートが、実行で許可されている必要があります。ポートのないURLは`6379`として扱います。アクションはほかのホストには接続しません。

読み取り専用として扱うコマンドは次のとおりです。`STORE`を付けた`SORT`のように、書き方によって読み取りにも書き込みにもなるコマンドは含みません。コマンドから効果を判定できないスクリプトと関数も含みません。`_RO`の形があるものは、そちらを使ってください。

| グループ | コマンド |
|-------|----------|
| キー | `EXISTS`, `TYPE`, `TTL`, `PTTL`, `EXPIRETIME`, `PEXPIRETIME`, `KEYS`, `SCAN`, `DBSIZE`, `RANDOMKEY`, `DUMP`, `SORT_RO`, `OBJECT ENCODING`, `OBJECT FREQ`, `OBJECT IDLETIME`, `OBJECT REFCOUNT`, `MEMORY USAGE` |
| 文字列とビット | `GET`, `MGET`, `STRLEN`, `GETRANGE`, `SUBSTR`, `LCS`, `GETBIT`, `BITCOUNT`, `BITPOS`, `BITFIELD_RO` |
| ハッシュ | `HGET`, `HMGET`, `HGETALL`, `HKEYS`, `HVALS`, `HLEN`, `HEXISTS`, `HSTRLEN`, `HSCAN`, `HRANDFIELD`, `HTTL`, `HPTTL`, `HEXPIRETIME`, `HPEXPIRETIME` |
| リスト | `LRANGE`, `LLEN`, `LINDEX`, `LPOS` |
| セット | `SMEMBERS`, `SISMEMBER`, `SMISMEMBER`, `SCARD`, `SRANDMEMBER`, `SSCAN`, `SINTER`, `SUNION`, `SDIFF`, `SINTERCARD` |
| ソート済みセット | `ZRANGE`, `ZRANGEBYSCORE`, `ZRANGEBYLEX`, `ZREVRANGE`, `ZREVRANGEBYSCORE`, `ZREVRANGEBYLEX`, `ZSCORE`, `ZMSCORE`, `ZCARD`, `ZCOUNT`, `ZLEXCOUNT`, `ZRANK`, `ZREVRANK`, `ZSCAN`, `ZRANDMEMBER`, `ZINTER`, `ZUNION`, `ZDIFF`, `ZINTERCARD` |
| ストリーム | `XRANGE`, `XREVRANGE`, `XLEN`, `XREAD`, `XPENDING`, `XINFO STREAM`, `XINFO GROUPS`, `XINFO CONSUMERS` |
| 地理 | `GEOPOS`, `GEODIST`, `GEOHASH`, `GEOSEARCH`, `GEORADIUS_RO`, `GEORADIUSBYMEMBER_RO` |
| 接続とサーバー | `PING`, `ECHO`, `TIME`, `INFO`, `SELECT`, `HELLO`, `CLIENT ID`, `CLIENT GETNAME`, `CLIENT INFO`, `CLIENT LIST`, `COMMAND`とその`COUNT`、`INFO`、`DOCS`、`LIST`、`GETKEYS`、`GETKEYSANDFLAGS`, `PUBSUB CHANNELS`, `PUBSUB NUMSUB`, `PUBSUB NUMPAT`, `PUBSUB SHARDCHANNELS`, `PUBSUB SHARDNUMSUB` |
| トランザクション | `MULTI`, `EXEC`, `DISCARD`, `WATCH`, `UNWATCH`。間に置いたコマンドは、ほかと同じように1つずつ確かめます |

`OBJECT`、`MEMORY`、`XINFO`、`CLIENT`、`COMMAND`、`PUBSUB`は`HELP`も受け付けます。`JSON.GET`や`FT.SEARCH`のようなモジュールのコマンドはこの一覧にないので、それを使うステップは`--allow-action`で許可する必要があります。

[ガード](/ja/guide/concepts/guard)を参照してください。

## 関連項目

- **[外部アクション](/ja/guide/concepts/actions#外部アクション)** - Probeが外部アクションを解決し、照合する仕組み
- **[Database](/ja/reference/actions/db)** - MySQL、PostgreSQL、SQLiteへのクエリ
- **[S3](/ja/reference/actions/s3)** - S3とS3互換ストレージのオブジェクト
