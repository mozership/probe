# GraphQLアクション

GraphQLアクションは、GraphQLのクエリやミューテーションをHTTPで送り、レスポンスの`data`と`errors`を分けて返します。

[外部アクション](/ja/guide/concepts/actions#外部アクション)として[mozership/probe-graphql](https://github.com/mozership/probe-graphql)で公開しています。ワークフローが初めて使うときにProbeがダウンロードし、固定したコミットの`action.yml`が示すSHA-256と一致する実行ファイルだけを実行します。Probe v1.17.0以降が必要です。ガードの下で実行するには、また`probe check`で`with`を検査するには、v1.21.0以降が必要です。

## 基本的な構文

ステップでは40文字のコミットSHAでアクションを固定します。各[リリース](https://github.com/mozership/probe-graphql/releases)のノートの先頭に、コピーして使う`uses`の行があります。

```yaml
steps:
  - name: Look up Japan
    uses: github.com/mozership/probe-graphql@41e4ffa222db58c63c7169117c919e6d252bdbf9 # v0.2.0
    with:
      url: https://countries.trevorblades.com/graphql
      query: |
        query Country($code: ID!) {
          country(code: $code) { name capital currency }
        }
      variables:
        code: JP
    test: res.code == 200 && len(res.errors) == 0 && res.data.country.capital == "Tokyo"
```

## パラメータ

| パラメータ | 型 | 必須 | デフォルト | 説明 |
|---|---|---|---|---|
| `url` | String | はい | - | GraphQLのエンドポイント。`http`または`https` |
| `query` | String | はい | - | クエリまたはミューテーションのドキュメント |
| `variables` | Object | いいえ | - | ドキュメントが宣言する変数の値 |
| `operation_name` | String | いいえ | - | ドキュメントに複数の操作があるときに実行する操作 |
| `headers` | Object | いいえ | - | `Authorization`などのリクエストヘッダー。下記の既定値を上書きします |
| `timeout` | Duration | いいえ | `30s` | リクエストの制限時間。`10s`のような形式か秒数で指定します。`0`を指定すると制限しません |

これら以外のキーを`with`に書くと、何も送る前にステップが失敗します。`action.yml`はこれらを`params`として宣言しているため、`probe check`がそのキーを行番号とともに報告します。

リクエストはJSONのボディを持つ`POST`で、`Content-Type: application/json`、`Accept: application/graphql-response+json, application/json`、`User-Agent: probe-graphql/<version>`を付けて送ります。

## レスポンスオブジェクト

| プロパティ | 型 | 説明 |
|---|---|---|
| `res.code` | Integer | HTTPステータスコード |
| `res.status` | String | `"200 OK"`のようなHTTPステータス行 |
| `res.headers` | Object | 正規化した名前をキーとするレスポンスヘッダー |
| `res.data` | Any | レスポンスの`data`。ないときは`null` |
| `res.errors` | Array | レスポンスの`errors`。ないときは空 |
| `res.body` | Any | レスポンスボディ全体。JSONなら解析した値、それ以外は文字列 |
| `res.rawbody` | String | 解析前のボディ。ボディがJSONのときに入ります |
| `req` | Object | 送った`url`、`query`、`variables`、`operation_name`、`headers` |
| `rt` | Duration | 往復の時間 |
| `status` | Integer | ステータスコードが2xxで、ボディがJSONで、`errors`が空なら`0`。それ以外は`1` |

サーバーが返したレスポンスはすべて結果として扱います。そのため、GraphQLのエラーや500もテストで確かめられます。接続の拒否やタイムアウトのように、レスポンスを得られなかったリクエストだけが、ステップをエラーとして失敗させます。

```yaml
steps:
  - name: An unknown field is reported in res.errors
    uses: github.com/mozership/probe-graphql@41e4ffa222db58c63c7169117c919e6d252bdbf9 # v0.2.0
    with:
      url: https://countries.trevorblades.com/graphql
      query: '{ country(code: "JP") { nope } }'
    test: status == 1 && len(res.errors) > 0
```

## ガードの下での動作

このアクションは実行のガードに従い、`action.yml`で`guard: [read-only, allow-host]`を宣言しています。そのため、どちらのガードの下でも`--allow-action`なしで実行します。拒否したステップは、何も送る前に種類`refused`で失敗します。

- `--read-only`の下では、クエリだけを送ります。実行する操作（`operation_name`が指すもの、または文書の中の唯一の操作）をGraphQLのパーサーで読み、ミューテーションとサブスクリプションは拒否します。文書を解析できない場合、操作が複数あるのに`operation_name`がない場合、`operation_name`が指す操作が文書にない場合も拒否します。
- `--allow-host`の下では、`url`のホストと、各リダイレクト先のホストが、実行が許可するものでなければなりません。ポートのないURLは、スキームの既定のポートとして扱います。

[ガード](/ja/guide/concepts/guard)を参照してください。

## 関連項目

- **[外部アクション](/ja/guide/concepts/actions#外部アクション)** - Probeが外部アクションを解決し、照合する仕組み
- **[HTTP](/ja/reference/actions/http)** - そのほかのHTTPリクエスト
- **[JMAP](/ja/reference/actions/jmap)** - Probeとあわせて公開しているもう1つの外部アクション
