# Fileアクション

`file`アクションは、パスにあるものを調べ、通常のファイルであれば読みます。サーバーがログに書いた内容、テスト対象が出力したファイルの内容、ファイルの権限や所有者を確認できます。`retry`と組み合わせれば、ファイルやソケットが現れるまで、または消えるまで待つこともできます。

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
| `path` | はい | | 調べるパス。ほかのアクションのパスと同じく、作業ディレクトリからの相対パスです。シンボリックリンクはたどります |

## レスポンス

| フィールド | 型 | 説明 |
|---|---|---|
| `res.exists` | Boolean | パスに何かがあれば`true`。何も指していないシンボリックリンクは、何もないものとして扱います |
| `res.type` | String | パスにあるものの種類。`file`、`dir`、`socket`、`pipe`、`device`、`other`のいずれかで、何もなければ`""` |
| `res.content` | String | 通常のファイルの内容をテキストのまま。それ以外では`""` |
| `res.mode` | String | 権限を`chmod`に渡す8進数の形で表したもの。setuid、setgid、スティッキービットを含みます。`0644`、`0600`、`4755`など |
| `res.owner` | String | 所有者のユーザー名。システムが名前を持たないユーザーでは番号 |
| `res.group` | String | 所有グループの名前。システムが名前を持たないグループでは番号 |
| `res.size` | Integer | システムが返すバイト数。ディレクトリの値はファイルシステムによって異なります |
| `res.modified` | String | 最終更新時刻。マシンのタイムゾーンでのRFC 3339の形式で、`2026-11-01T09:30:00.5+09:00`のようになります。`date`で時刻として読めます |
| `req.path` | String | 指定したパス |
| `status` | Integer | パスに何かがあれば`0`、なければ`1` |

読むのは通常のファイルだけです。ディレクトリ、ソケット、パイプ、デバイスはあることだけを返し、`res.content`は空です。パイプは開きません。開くと書き手を待ってしまうためです。

パスに何もない場合、`res.mode`、`res.owner`、`res.group`、`res.modified`は`""`、`res.size`は`0`です。シンボリックリンクはたどるので、これらの値はリンク先のものです。

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

## ガードの下で

このアクションは読むだけで、どこにも接続しません。そのため、`--read-only`でも`--allow-host`でもそのまま動きます。詳しくは[ガード](/ja/guide/concepts/guard)を参照してください。

## エラー処理

パスに何もない場合と、通常のファイルの下のパスを指定した場合は、`res.exists`が`false`、`status`が`1`の結果を返します。ログのローテーションなどで、読んでいる間にファイルが置き換わった場合は、3回まで調べ直します。次の場合、ステップはアクションのエラーで失敗します。

- `path`を指定していない
- パスにあるものを調べられない、または読めない。読む権限のないファイルなど
- 通常のファイルが1,000,000バイトを超える。式の`file`関数が読める大きさと同じです。超えた分を切り捨てて読むことはしません
