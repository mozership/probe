# Fileアクション

`file`アクションは、パスにあるものを調べ、通常のファイルであれば読みます。サーバーがログに書いた内容、テスト対象が出力したファイルの内容、ファイルの権限や所有者を確認できます。`retry`と組み合わせれば、ファイルやソケットが現れるまで、または消えるまで待つこともできます。`op`を指定すれば、ファイルを書く、末尾に追記する、削除することもできます。テストに使うファイルを用意したり片付けたりするのに、`shell`は要りません。

## 基本構文

Fileのステップにはパスを指定し、`test`で`res`からファイルの内容を読みます。

```yaml
steps:
  - name: No error is logged
    uses: file
    with:
      path: ./server.log
    test: res.exists && !(res.content contains "level=ERROR")
```

## パラメータ

| キー | 必須 | デフォルト | 説明 |
|---|---|---|---|
| `path` | はい | | 調べるパス。ほかのアクションのパスと同じく、作業ディレクトリからの相対パスです。`delete`以外では、シンボリックリンクはたどります |
| `op` | いいえ | `read` | パスに対して行う操作。`read`、`write`、`append`、`delete`のいずれかです。[書き込み、追記、削除](#書き込み追記削除)を参照してください |
| `content` | いいえ | `""` | `write`で書く内容、または`append`で末尾に足す内容 |
| `mode` | いいえ | | `write`と`append`で設定する権限。`chmod`に渡す8進数を引用符で囲んで書きます。`"0600"`など |
| `recursive` | いいえ | `false` | `delete`で、ディレクトリを中身ごと削除します |

## レスポンス

| フィールド | 型 | 説明 |
|---|---|---|
| `res.exists` | Boolean | パスに何かがあれば`true`。何も指していないシンボリックリンクは、何もないものとして扱います |
| `res.type` | String | パスにあるものの種類。`file`、`dir`、`socket`、`pipe`、`device`、`other`のいずれかで、何もなければ`""` |
| `res.content` | String | 通常のファイルの内容をテキストのまま。それ以外では`""` |
| `res.mode` | String | 権限を`chmod`に渡す8進数の形で表したもの。setuid、setgid、スティッキービットを含みます。`0644`、`0600`、`4755`など |
| `res.owner` | String | 所有者のユーザー名。名前のないユーザーでは番号。下の説明も参照してください |
| `res.group` | String | 所有グループの名前。名前のないグループでは番号。下の説明も参照してください |
| `res.size` | Integer | システムが返すバイト数。ディレクトリの値はファイルシステムによって異なります |
| `res.modified` | String | 最終更新時刻。マシンのタイムゾーンでのRFC 3339の形式で、`2026-11-01T09:30:00.5+09:00`のようになります。`date`で時刻として読めます |
| `req.path` | String | 指定したパス |
| `req.op` | String | 行った操作。指定がなければ`read` |
| `req.content` | String | 指定した内容。指定がなければ`""` |
| `req.mode` | String | 指定した権限。指定がなければ`""` |
| `req.recursive` | Boolean | `recursive`に`true`を指定したかどうか |
| `status` | Integer | パスに何かがあれば`0`、なければ`1` |

どの操作でも、`res`は操作を終えたあとのパスの状態を、`read`と同じ形で返します。`write`のあとは書いたファイル、`delete`のあとは何もない状態です。

読むのは通常のファイルだけです。ディレクトリ、ソケット、パイプ、デバイスはあることだけを返し、`res.content`は空です。パイプは開きません。開くと書き手を待ってしまうためです。

パスに何もない場合、`res.mode`、`res.owner`、`res.group`、`res.modified`は`""`、`res.size`は`0`です。シンボリックリンクはたどるので、これらの値はリンク先のものです。

Linuxでは、ユーザーとグループの名前を`/etc/passwd`と`/etc/group`からだけ読みます。LDAPなどのディレクトリサービスがNSS経由で名前を付けているユーザーやグループは、`ls -l`では名前が表示されても、Probeでは番号になります。そのようなホストでは番号と比べてください。macOSではシステムに問い合わせるので、ディレクトリサービスの名前も得られます。

`res.content`は、JSONのように見える内容でもファイルのテキストのままです。HTTPのレスポンスの`body`とは扱いが異なり、JSON Lines形式のログも検索できるテキストとして読めます。JSONのファイルは`parse_json`で読みます。

## 例

### サーバーの起動を待つ

ログにサーバーの起動が書かれるまで、ステップを繰り返します。

```yaml
- name: Wait for the server to start
  uses: file
  with:
    path: ./server.log
  retry:
    max_attempts: 60
    interval: 1s
  test: res.content contains "server started"
```

### 行を数える

`lines`で`res.content`を行のリストに分ければ、リストを受け取る式の関数に渡せます。

```yaml
- name: Two workers are started
  uses: file
  with:
    path: ./server.log
  test: 'count(lines(res.content), # contains "Worker started") == 2'
```

引用符で始まる`test`や、` #`を含む`test`は引用符で囲みます。囲まないと、YAMLが`#`以降をコメントとして読みます。

### テスト対象が書いたファイル

`res.content`はテキストです。JSONのファイルは`parse_json`で読みます。

```yaml
- name: The export holds every order
  uses: file
  with:
    path: ./out/orders.json
  test: len(parse_json(res.content).orders) == 3
```

### 権限と所有者

```yaml
- name: The key is private to the app
  uses: file
  with:
    path: /etc/app/key.pem
  test: res.mode == "0600" && res.owner == "app" && res.group == "app"
```

### 最近更新されたか

`res.modified`は`date`で時刻として読めます。時刻から時刻を引くと期間になります。

```yaml
- name: The backup ran in the last day
  uses: file
  with:
    path: /var/backups/db.sql.gz
  test: res.size > 0 && now() - date(res.modified) < duration("24h")
```

### 終了時にソケットが消える

パスに何もないことはステップの失敗ではなく、`test`で確かめられる結果です。そのため、ファイルが消えるのを待つステップも書けます。

```yaml
- name: The socket is removed
  uses: file
  with:
    path: /run/app.sock
  retry:
    max_attempts: 15
    interval: 2s
  test: '!res.exists'
```

## 書き込み、追記、削除

`op`を指定すると、ステップはパスを調べる前にその操作を行います。

| `op` | 行うこと |
|---|---|
| `read` | 何もしません。調べるだけです |
| `write` | ファイルの内容を`content`で置き換えます。ファイルや、それを置くディレクトリがなければ作ります |
| `append` | ファイルの末尾に`content`を足します。ファイルや、それを置くディレクトリがなければ作ります |
| `delete` | パスにあるものを削除します。ディレクトリは、空の場合か、`recursive: true`を指定した場合に中身ごと削除します |

`write`と`append`が作るファイルの権限は`0644`、ディレクトリは`0755`から、実行時のumaskを除いたものです。シェルで作った場合と同じです。`mode`を指定すると、新しく作ったファイルにも既存のファイルにも、その権限を設定します。指定しなければ、既存のファイルは元の権限のままです。`mode`は引用符で囲んで書きます。`0600`のような数値は、YAMLのパーサーによって10進数とも8進数とも読まれるため、エラーにしています。`write`と`append`は、シェルの`>`と同じくシンボリックリンクをたどります。書くのは通常のファイルだけで、ディレクトリやパイプなどがあればエラーです。`content`はテキストです。数値や真偽値は見たとおりに書きます。マップやリストはエラーです。前のステップのJSONの`body`全体などを、Probe独自の形で書いてしまわないためです。実行ユーザーが読めない権限（`"0200"`など）を付けたファイルは、内容なしで結果を返します。

`delete`はシンボリックリンクそのものを削除し、リンク先は残します。末尾にスラッシュを付けても同じです。`..`は、パスの書かれたとおりに、直前に書いたディレクトリを含むディレクトリを指します。直前のものがシンボリックリンクでも同じです。`rm -f`と同じく、パスに何もなくてもエラーにはしません。そのため、あるかどうかわからないものを片付けるステップにも使えます。作業ディレクトリと、それを含むディレクトリ（`/`など）は削除しません。シンボリックリンク経由のパスで指定しても同じです。変数が空になってできたパスで、それらを消してしまわないためです。

`content`、`mode`、`recursive`は、それを使う操作でだけ受け付けます。たとえば`read`に`content`を指定すると、無視せずエラーにします。

### ファイルを作り、片付ける

```yaml
jobs:
- name: Start with a config
  id: config
  steps:
  - name: Write the config
    uses: file
    with:
      path: ./tmp/app.yml
      op: write
      content: |
        port: 8080
        log: ./tmp/app.log
      mode: "0600"
    test: res.exists && res.mode == "0600"

- name: Clean up
  needs: [config]
  always: true
  steps:
  - name: Remove what the run made
    uses: file
    with:
      path: ./tmp
      op: delete
      recursive: true
    test: '!res.exists'
```

### サーバーが監視するログ

```yaml
- name: A line comes into the log
  uses: file
  with:
    path: ./watched.log
    op: append
    content: |
      level=ERROR msg="disk full"
  test: res.exists
```

## ガードの下で

このアクションはどこにも接続しないので、`--allow-host`ではそのまま動きます。`--read-only`では読むだけです。`write`、`append`、`delete`のステップは、何も変えないうちに種類`refused`で拒否します。詳しくは[ガード](/ja/guide/concepts/guard)を参照してください。

## エラー処理

パスに何もない場合と、通常のファイルの下のパスを指定した場合は、`res.exists`が`false`、`status`が`1`の結果を返します。ログのローテーションなどで、読んでいる間にファイルが置き換わった場合は、3回まで調べ直します。次の場合、ステップはアクションのエラーで失敗します。

- `path`を指定していない
- パスにあるものを調べられない、または読めない。読む権限のないファイルなど
- 通常のファイルが1,000,000バイトを超える。式の`file`関数が読める大きさと同じです。超えた分を切り捨てて読むことはしません。`write`や`append`でこの大きさを超えた場合は、書いたあと、読むところで失敗します
- `op`が4つのいずれでもない
- `content`、`mode`、`recursive`を、それを受け付けない操作に指定した
- `mode`が、引用符で囲んだ8進数でない
- `content`がマップやリストである
- `write`や`append`で、パスが区切り文字で終わる、パスに通常のファイル以外のものがある、または書けない
- `delete`で、空でないディレクトリに`recursive`を指定していない、または作業ディレクトリかそれを含むディレクトリを指定した
