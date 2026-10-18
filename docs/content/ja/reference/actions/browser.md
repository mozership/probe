# Browserアクション

Browserアクションは[chromedp](https://github.com/chromedp/chromedp)を通して実際のChromeを操作します。ページを開き、内容を読み、入力し、要素を待ち、スクリーンショットを撮ります。Probeを実行する環境にChromeかChromiumが必要です。

[外部アクション](/ja/guide/concepts/actions#外部アクション)として[mozership/probe-browser](https://github.com/mozership/probe-browser)で公開しています。Probe v1.21.0までは組み込みアクションでした。ワークフローが初めて使うときにProbeがダウンロードし、固定したコミットの`action.yml`が示すSHA-256と一致する実行ファイルだけを実行します。Probe v1.21.0以降が必要です。各[リリース](https://github.com/mozership/probe-browser/releases)のノートの先頭に、コピーして使う`uses`の行があります。

このアクションはページが何をするかを判定できないため、[ガード](/ja/guide/concepts/guard)に従いません。`--read-only`や`--allow-host`の下では、`--allow-action`で指定しない限り、このアクションを使うステップを拒否します。

## 基本的な構文

browserステップでは、行う操作を`actions`に順番に並べます。ステップごとに新しいブラウザを起動し、操作を実行して閉じます。そのためクッキーやログイン状態は次のステップに引き継がれません。一連の流れは1つのステップにまとめます。

```yaml
steps:
  - name: Read the heading
    uses: github.com/mozership/probe-browser@42dda412d5d183c091f8baf071237cc090f16052 # v0.1.0
    with:
      actions:
        - name: navigate
          url: "{{vars.url}}/"
        - name: text
          id: heading
          selector: h1
    test: res.code == 0 && res.results.heading == "Welcome"
```

## パラメータ

以下は`with`の直下に書きます。

| パラメータ | 型 | デフォルト | 説明 |
|-----------|------|---------|-------------|
| `actions` | List | 必須 | 順に実行する操作。[アクション](#アクション)を参照 |
| `timeout` | 期間の文字列、または秒数 | `5s` | ステップ全体の制限時間。ブラウザの起動とすべての操作を含む。`30s`、`30`、`"30"`はどれも30秒。それ以外の値や0以下の値はエラーになる |
| `headless` | Boolean | `true` | ウィンドウを表示せずにChromeを動かす |
| `window_w` | Integer | `1920` | ウィンドウの幅（ピクセル） |
| `window_h` | Integer | `1080` | ウィンドウの高さ（ピクセル） |
| `evidence_dir` | String | 一時ディレクトリ | 操作が失敗したときにページを保存する場所。[失敗時のページ](#失敗時のページ)を参照 |

Chromeの起動にも`timeout`の一部を使い、1秒以上かかることもよくあります。短すぎる制限では、最初のページを開く前に時間切れになることがあります。

## アクション

`actions`の各要素は、`name`で操作を指定し、次のフィールドを取ります。

| フィールド | 使うアクション | 説明 |
|-------|---------|-------------|
| `name` | すべて | 下の表にある操作の名前 |
| `id` | 結果を返すアクション | `res.results`や`res.filepaths`で結果を引くキー。省くとアクション名がキーになるため、同じ種類のアクションを後に置くと前の結果を置き換える |
| `url` | `navigate` | 開くアドレス |
| `selector` | 要素に対するアクション | CSSセレクタ |
| `value` | `type`、`send_keys`、`select` | 使うテキスト |
| `attribute` | `get_attribute` | 読む属性を`href`か`[href]`で指定する。リストの場合は最初の要素だけを読む |
| `quality` | `full_screenshot` | `1`から`99`はその品質のJPEGで保存する。それ以外は、省略、`0`、`100`以上、負の数も含めてPNGで保存する |
| `path` | スクリーンショット | 非推奨。画像をこのパスにも書き出す。代わりに`res.filepaths`を使う |

要素を探すアクションは、要素が現れるまで待ちます。先に`timeout`が尽きるとステップは失敗します。多くは要素が表示されるまでも待ちます。

### 移動と待機

ページを開くか、次の操作に必要な状態になるまでステップを待たせます。

| アクション | 内容 |
|--------|--------------|
| `navigate` | `url`を開き、ページの読み込みを待つ。`net::ERR_CONNECTION_REFUSED`のようにブラウザがネットワークのエラーを返すとすぐに失敗する。応答のないアドレスでは`timeout`まで待つ |
| `wait_visible` | `selector`が表示されるまで待つ |
| `wait_not_visible` | `selector`が表示されなくなるまで待つ |
| `wait_enabled` | `selector`が表示され、有効になるまで待つ |
| `wait_ready` | ページの`body`の準備ができるまで待つ。`selector`は使わない |

### ページの読み取り

以下は文字列を`res.results`に、`id`またはアクション名をキーにして入れます。

| アクション | 結果 |
|--------|--------|
| `text` | `selector`のテキスト |
| `wait_text` | `selector`が表示されるまで待ち、そのテキスト |
| `value` | `input`などのフォーム項目の値 |
| `get_attribute` | `selector`の、`attribute`の最初の属性の値。要素がその属性を持たなければ空文字列 |
| `get_html` | 表示された`selector`の外側のHTML |

### 操作

利用者と同じようにページを操作します。結果は返しません。

| アクション | 内容 |
|--------|--------------|
| `click` | 表示された`selector`をクリックする |
| `double_click` | 表示された`selector`をダブルクリックする |
| `type`、`send_keys` | `selector`の内容を消してから`value`を入力する |
| `submit` | `selector`が属するフォームを送信する |
| `focus` | `selector`にフォーカスを移す |
| `scroll` | `selector`が見える位置までスクロールする |
| `select` | `selector`の`value`属性を`value`に設定する。`<select>`の選択肢は選ばないため、選ぶ場合は選択肢をクリックする |
| `hover` | `selector`に`mouseover`イベントを送る。待たず、一致する要素がなければ何もしない |
| `right_click` | `selector`に`contextmenu`イベントを送る。待たず、一致する要素がなければ何もしない |

### スクリーンショット

以下は画像をファイルに保存し、そのパスを`res.filepaths`に、`id`またはアクション名をキーにして入れます。ファイル名の拡張子は形式に合わせて`.png`か`.jpg`になります。

| アクション | 画像 |
|--------|-------|
| `capture_screenshot` | ウィンドウに見えている範囲。PNG |
| `full_screenshot` | ページ全体。PNG。`quality`が`1`から`99`ならJPEG |
| `screenshot` | 表示された`selector`だけ。PNG |

## レスポンスオブジェクト

すべての操作が成功すると、ステップからは次の値を参照できます。

| フィールド | 型 | 説明 |
|-------|------|-------------|
| `res.code` | Integer | `0` |
| `res.results` | Object | 読み取りのアクションが返した値。キーは`id`またはアクション名 |
| `res.filepaths` | Object | スクリーンショットの保存先。キーは`id`またはアクション名 |
| `rt.duration` | String | ステップにかかった時間。`"663.268333ms"`など |
| `status` | Integer | `0` |

操作が失敗した場合は、テストできるレスポンスはありません。ステップは[エラーハンドリング](#エラーハンドリング)のとおりアクションのエラーとして失敗します。

## 例

どの例も1つのステップです。1つのステップの操作は同じブラウザを共有し、次のステップは新しいブラウザで始まるためです。

### ログイン

ログインと、それが成功したことの確認は同じブラウザで行う必要があるため、1つのステップにします。

```yaml
secrets:
  - PASSWORD
vars:
  url: "{{APP_URL}}"
  user: "{{USERNAME}}"
  password: "{{PASSWORD}}"

jobs:
- name: Sign in
  steps:
  - name: Log in and reach the dashboard
    uses: github.com/mozership/probe-browser@42dda412d5d183c091f8baf071237cc090f16052 # v0.1.0
    with:
      timeout: 30s
      actions:
        - name: navigate
          url: "{{vars.url}}/login"
        - name: type
          selector: "#username"
          value: "{{vars.user}}"
        - name: type
          selector: "#password"
          value: "{{vars.password}}"
        - name: click
          selector: "button[type='submit']"
        - name: wait_visible
          selector: "#dashboard"
        - name: text
          id: greeting
          selector: "#dashboard h1"
    test: res.code == 0 && res.results.greeting contains vars.user
```

### 複数の値の読み取り

結果が互いに置き換わらないよう、読み取りのアクションにはそれぞれ`id`を付けます。

```yaml
  - name: Product page
    uses: github.com/mozership/probe-browser@42dda412d5d183c091f8baf071237cc090f16052 # v0.1.0
    with:
      actions:
        - name: navigate
          url: "{{vars.url}}/products/42"
        - name: text
          id: title
          selector: "h1"
        - name: text
          id: price
          selector: ".price"
        - name: get_attribute
          id: image
          selector: "img.product"
          attribute: [src]
    test: |
      res.code == 0 &&
      res.results.title != "" &&
      res.results.price startsWith "$"
    outputs:
      image_url: res.results.image
```

### スクリーンショットを残す

保存した画像のパスは`res.filepaths`に入り、表示したり次に渡したりできます。

```yaml
  - name: Checkout page
    uses: github.com/mozership/probe-browser@42dda412d5d183c091f8baf071237cc090f16052 # v0.1.0
    with:
      window_w: 1280
      window_h: 800
      actions:
        - name: navigate
          url: "{{vars.url}}/checkout"
        - name: full_screenshot
          id: page
    test: res.code == 0
    echo: "Saved {{res.filepaths.page}}"
```

## エラーハンドリング

アクションが完了できないと、ステップ全体がアクションのエラーとして失敗し、実行は終了ステータス`3`で終わります。セレクタが何にも一致しなくても空の結果にはなりません。`wait_visible`、`text`、`click`など要素を探すアクションは、ステップの`timeout`まで待ち続けます。ナビゲーションは、接続の拒否のようにブラウザがネットワークのエラーを返すとすぐに失敗しますが、応答のないアドレスでは`timeout`まで待ちます。いずれの場合もテストできる`res`はなく、失敗のメッセージにブラウザのエラーが入ります。

### 失敗時のページ

アクションが失敗すると、Probeはその時点のページを保存します。ページ全体のPNGのスクリーンショットと、文書のHTMLです。2つのファイルには`probe-browser-failure-1947177907.png`と`.html`のように同じ名前が付きます。失敗のメッセージには、ページのURLとあわせて両方のファイルのパスが入ります。

```
action error in step_execute: action execution failed (caused by: ... context deadline exceeded (page at failure: url http://localhost:8090/, screenshot /tmp/probe-browser-failure-1947177907.png, html /tmp/probe-browser-failure-1947177907.html))
```

`--report`で書き出すファイルでも、このメッセージが失敗の`message`になります。テストがfalseになるのはアクションの失敗ではないため、そのときはページを保存しません。残したい場合は`capture_screenshot`アクションを加えます。

ファイルはシステムの一時ディレクトリに保存します。`evidence_dir`を指定するとそこに保存し、ディレクトリは必要に応じて作成します。CIのアーティファクトとしてアップロードする場合に便利です。ページには個人の情報が写りうるため、ファイルはProbeを実行したユーザーだけが読めるようにします。

```yaml
- name: Checkout page
  uses: github.com/mozership/probe-browser@42dda412d5d183c091f8baf071237cc090f16052 # v0.1.0
  with:
    evidence_dir: out/browser
    actions:
    - name: navigate
      url: "{{vars.url}}/checkout"
    - name: wait_visible
      selector: "#pay"
```

ページを読み取れるように、ブラウザはタイムアウトのあとも最大10秒残します。ブラウザが起動しなかった場合や、時間切れの時点でページがまだ読み込み中だった場合など、読み取れなかったときはその旨をメッセージに書き、何も保存しません。

## CIでの実行

CIのランナーには、ブラウザと、手元より少し長い時間が必要です。

- ランナーにChromeかChromiumを入れる。
- `headless: true`のままにするか、ウィンドウが必要ならXvfbなどのディスプレイを用意する。
- コンテナで多く必要になるため、Chromeは`--no-sandbox`で起動する。信頼できるサイトだけを開く。
- コールドスタートのランナーではChromeの起動が遅いため、`timeout`に余裕を持たせる。
