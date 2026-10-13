# Containerアクション

Containerアクションは、後続のステップのためにコンテナを起動し、実行が終わると削除します。テスト対象のAPIが必要とするデータベースやメールサーバーを、ワークフローの中で用意できます。

[外部アクション](/ja/guide/concepts/actions#外部アクション)として[mozership/probe-container](https://github.com/mozership/probe-container)で公開しています。ワークフローが初めて使うときにProbeがダウンロードし、固定したコミットの`action.yml`が示すSHA-256と一致する実行ファイルだけを実行します。[実行の終了後にアクションを呼び出す](/ja/guide/concepts/actions#実行の後始末)Probe v1.27.0以降と、`docker`コマンドが必要です。同じ引数を受け付ける[別のランタイム](#ランタイム)も使えます。

## 基本的な構文

ワークフローでは40文字のコミットSHAでアクションを固定します。各[リリース](https://github.com/mozership/probe-container/releases)のノートの先頭に、コピーして使うアクションとコミットがあります。このページの例では、[`actions`](/ja/reference/yaml-configuration#actions)で一度だけ名前を付けています。

```yaml
name: Orders
actions:
  container: github.com/mozership/probe-container@cd5c071a029cc1c6d4afe31750bf33ec6d9aa5b8 # v0.1.0
jobs:
  - name: orders
    steps:
      - name: Start Postgres
        id: pg
        uses: container
        with:
          image: postgres:17
          env:
            POSTGRES_PASSWORD: "{{vars.pg_password}}"
          ports: [5432]
          wait:
            port: 5432
        outputs:
          addr: res.addr

      - name: The schema is empty to begin with
        uses: db
        with:
          dsn: "postgres://postgres:{{vars.pg_password}}@{{outputs.pg.addr}}/postgres?sslmode=disable"
          query: SELECT count(*) AS n FROM pg_tables WHERE schemaname = 'public'
        test: res.rows[0].n == 0
```

ステップは、コンテナを起動した時点で終わります。`wait`があれば、準備ができるまで待ってから終わります。コンテナはそのジョブと他のジョブの後続のステップのために動き続け、実行が終わると匿名ボリュームとともに削除されます。実行が成功しても、失敗しても、Ctrl+Cで止めても同じです。そのために書くものはありません。

## パラメータ

| パラメータ | 型 | 必須 | デフォルト | 説明 |
|-----------|------|------|-----------|------|
| `image` | String | はい | - | 起動するイメージ |
| `command` | List | いいえ | イメージのコマンド | コンテナで実行するコマンド。`[redis-server, --appendonly, "yes"]`のように、1語を1要素として書きます |
| `env` | Map | いいえ | - | コンテナの環境変数。名前と値の組で書きます |
| `ports` | List | いいえ | - | マシンから届くようにするコンテナのポート。[ポート](#ポート)を参照してください |
| `volumes` | List | いいえ | - | マウントするもの。`/abs/path/init.sql:/docker-entrypoint-initdb.d/init.sql:ro`のように、ランタイムの`--volume`が受け付ける形で書きます |
| `network` | String | いいえ | ランタイムのデフォルト | コンテナをつなぐネットワーク。既にあるものを指定します。アクションはネットワークを作りません |
| `name` | String | いいえ | ランタイムが付ける名前 | コンテナの名前。同時に動く2つの実行が同じ名前のコンテナを持つことはできないので、必要がなければ指定しません |
| `pull` | String | いいえ | `missing` | イメージを取得する条件。`missing`、`always`、`never`のいずれか |
| `wait` | Map | いいえ | - | ステップが終わる前にコンテナが満たす条件。[待機](#待機)を参照してください |
| `keep` | Boolean | いいえ | `false` | 実行が終わってもコンテナを残します。[コンテナを残す](#コンテナを残す)を参照してください |

これら以外のキーを`with`に書くと、何も起動しないうちにステップが失敗します。`action.yml`がこれらを`params`として宣言しているので、`probe check`はそのようなキーを行番号とともに報告します。

アクションは`env`の値を結果にもログにも出さず、変数の名前だけを出します。値はランタイムに引数として渡すので、コンテナの起動中はマシンのプロセス一覧から見え、起動後はランタイムが表示するコンテナの情報からも見えます。

## ポート

`ports`の各要素は、コンテナのポートです。

| 要素 | 公開先 |
|------|--------|
| `5432` | `127.0.0.1`の空いているポート。結果がその番号を返します |
| `53/udp` | 同じく空いているポート。TCP以外のプロトコル(`udp`または`sctp`)で公開します |
| `"8080:80"`、`"0.0.0.0:8080:80"` | 要素に書いた場所。ランタイムの`--publish`が受け付ける形で書きます |

ポートだけを書く形を使ってください。ランタイムが空いているポートを選ぶので、同じマシンで動く2つの実行や、1つの実行の2つのジョブがぶつかりません。ポートがネットワークに開くこともありません。公開先は[結果](#レスポンスオブジェクト)から読みます。

## 待機

`wait`がないと、ステップはコンテナを起動した時点で終わります。その時点では、中で動くものの準備はまだできていません。`wait`は、準備ができるまでステップを終わらせません。

| キー | 型 | 説明 |
|------|------|------|
| `port` | Number | `ports`にあるTCPのポート。接続を受け付け、保ち続けることを待ちます。ランタイムは、コンテナの中で何も待ち受けていなくても公開したポートで接続を受け付け、すぐに閉じます。そのため、受け付けられただけの接続は数えません |
| `log` | String | [正規表現](https://pkg.go.dev/regexp/syntax)。コンテナがそれまでに標準出力と標準エラーに書いた内容の全体と照合します。全体と照合するので、`(?s)ready.*ready`と書けば、2回書かれる行を待てます |
| `healthcheck` | Boolean | `true`にすると、イメージのヘルスチェックが通るまで待ちます。ヘルスチェックのないイメージではステップが失敗します |
| `timeout` | Duration | 待つ時間。`90s`のような形か、秒数で書きます。デフォルトは`60s`です |

複数を指定すると、すべてが満たされるまで待ちます。イメージの取得にかかる時間は、タイムアウトに含まれません。

待っている間にコンテナが止まるか、時間内に準備ができないと、ステップは失敗します。エラーの末尾には、コンテナのログの最後の数行が付きます。コンテナは、実行が終わるときに他のコンテナとともに削除されます。

データベースのように、応答できるようになる前から接続を受け付けるサーバーは、ログかヘルスチェックで待つほうが確実です。

```yaml
steps:
  - name: Start Postgres, and wait until it is up for good
    uses: container
    with:
      image: postgres:17
      env:
        POSTGRES_PASSWORD: "{{vars.pg_password}}"
      ports: [5432]
      wait:
        # The image starts the server once to set it up, and then again.
        log: "(?s)ready to accept connections.*ready to accept connections"
        timeout: 90s
```

## レスポンスオブジェクト

| プロパティ | 型 | 説明 |
|-----------|------|------|
| `res.id` | String | コンテナのID |
| `res.name` | String | コンテナの名前。同じ`network`にある別のコンテナは、この名前で接続します |
| `res.ports` | Object | `ports`の各要素に対する、マシン側のポート。コンテナのポートをキーにして、`res.ports["5432"]`のように読みます。TCP以外のプロトコルでは`res.ports["53/udp"]`です |
| `res.host` | String | `ports`の最初の要素に届くアドレス。要素が別のアドレスを指定していなければ`127.0.0.1`です |
| `res.port` | Integer | `ports`の最初の要素に対する、マシン側のポート |
| `res.addr` | String | `res.host`と`res.port`を合わせたもの。`127.0.0.1:51110`の形です |
| `req` | Object | コンテナの起動に使った内容。`env`は変数の名前だけです |
| `rt` | Duration | ステップにかかった時間 |
| `status` | Integer | `0` |

`res.host`、`res.port`、`res.addr`は、ステップに`ports`があるときだけ返ります。

起動できないコンテナや準備ができないコンテナは、結果ではなくエラーになります。そのため、`test`の内容にかかわらずステップは失敗します。

## コンテナを残す

テストが失敗したときなど、実行のあとでコンテナの中を調べたい場合は、コンテナを残せます。

- `PROBE_CONTAINER_KEEP=1 probe workflow.yml`は、ワークフローを書き換えずに、その実行のコンテナをすべて残します。
- ステップに`keep: true`を書くと、そのステップのコンテナだけを残します。

実行が終わると、アクションは残したコンテナと、それを削除するコマンドを表示します。

```
[WARN]  actions.probe-container: kept containers of the run: run_id=88041310b4926f7b count=1 remove="docker rm --force --volumes $(docker ps --all --quiet --filter label=probe.run_id=88041310b4926f7b)"
[WARN]  actions.probe-container: kept container: name=sleepy_dubinsky image=postgres:17 ports=127.0.0.1:51110->5432/tcp
```

アクションが起動するコンテナには、実行のIDを値とするラベル`probe.run_id`が付きます。残すコンテナには`probe.keep=true`も付きます。`docker ps --all --filter label=probe.run_id`で、すべての実行が残したコンテナを一覧できます。

## 残るもの

Probeは実行が終わるとアクションを呼び出し、アクションはその実行のラベルが付いたコンテナをすべて削除します。コンテナが残るのは、次の場合だけです。

- ステップまたは環境変数が、コンテナを[残す](#コンテナを残す)よう指定している。
- `kill -9`などでProbeが強制終了され、後始末まで進めなかった。
- ランタイムが25秒以内にコンテナを削除できなかった。この場合、Probeが警告を表示します。

名前付きのボリュームとネットワークは、アクションが作ったものではないので残ります。

## ランタイム

アクションは`docker`を実行します。`PROBE_CONTAINER_RUNTIME`には、`podman`や`nerdctl`のように同じ引数を受け付ける別のコマンドか、そのパスを指定できます。

```bash
PROBE_CONTAINER_RUNTIME=podman probe workflow.yml
```

ランタイムはステップではなく環境変数で指定します。ランタイムはワークフローではなくマシンに属するものであり、実行の後始末は、ステップが使ったのと同じランタイムで行う必要があるためです。

アクションはイメージをビルドせず、既にあるコンテナの中で何かを実行することもありません。どちらも、[`shell`](/ja/reference/actions/shell)のステップからランタイムを直接呼べば行えます。

## ガードの下での動作

アクションはどのガードも守りません。コンテナの中で動くものや、それが接続するホストは、アクションが制限できるものではないためです。`--read-only`または`--allow-host`の下では、`--allow-action`でアクションを指定しない限り、ステップは拒否されます。

[ガード](/ja/guide/concepts/guard)を参照してください。

## 関連項目

- **[外部アクション](/ja/guide/concepts/actions#外部アクション)** - Probeが外部アクションを解決し、照合する仕組み
- **[実行の後始末](/ja/guide/concepts/actions#実行の後始末)** - Probeが実行の終了後にアクションを呼び出す仕組み
- **[Database](/ja/reference/actions/db)** - MySQL、PostgreSQL、SQLiteへのクエリ
- **[Redis](/ja/reference/actions/redis)** - RedisまたはValkeyのサーバーでのコマンド
