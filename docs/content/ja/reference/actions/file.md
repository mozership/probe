# Fileアクション

`file`アクションは、パスにあるものを調べ、通常のファイルであれば読みます。サーバーがログに書いた内容、テスト対象が出力したファイルの内容を確認できます。`retry`と組み合わせれば、ファイルやソケットが現れるまで、または消えるまで待つこともできます。

## 基本構文

Fileのステップにはパスを指定し、`test`で`res`からファイルの内容を読みます。

```yaml
steps:
  - name: No error is logged
    uses: file
    with:
      path: ./server.log
    test: res.exists && !(res.body contains "level=ERROR")
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
| `res.body` | String | 通常のファイルの内容。それ以外では`""` |
| `req.path` | String | 指定したパス |
| `status` | Integer | パスに何かがあれば`0`、なければ`1` |

読むのは通常のファイルだけです。ディレクトリ、ソケット、パイプ、デバイスはあることだけを返し、`res.body`は空です。パイプは開きません。開くと書き手を待ってしまうためです。

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
  test: res.body contains "server started"
```

### 行を数える

`lines`で`res.body`を行のリストに分ければ、リストを受け取る式の関数に渡せます。

```yaml
- name: Two workers are started
  uses: file
  with:
    path: ./server.log
  test: 'count(lines(res.body), # contains "Worker started") == 2'
```

引用符で始まる`test`や、` #`を含む`test`は引用符で囲みます。囲まないと、YAMLが`#`以降をコメントとして読みます。

### テスト対象が書いたファイル

`res.body`は文字列です。JSONのファイルは`parse_json`で読みます。

```yaml
- name: The export holds every order
  uses: file
  with:
    path: ./out/orders.json
  test: len(parse_json(res.body).orders) == 3
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

パスに何もない場合と、通常のファイルの下のパスを指定した場合は、`res.exists`が`false`、`status`が`1`の結果を返します。次の場合、ステップはアクションのエラーで失敗します。

- `path`を指定していない
- パスにあるものを調べられない、または読めない。読む権限のないファイルなど
- 通常のファイルが1,000,000バイトを超える。式の`file`関数が読める大きさと同じです。超えた分を切り捨てて読むことはしません
