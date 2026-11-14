# Browserアクション

Browserアクションは[chromedp](https://github.com/chromedp/chromedp)を通して実際のChromeを操作します。ページを開き、内容を読み、入力し、要素を待ち、スクリーンショットを撮ります。Probeを実行する環境にChromeかChromiumが必要です。

[外部アクション](/ja/guide/concepts/actions#外部アクション)として[mozership/probe-browser](https://github.com/mozership/probe-browser)で公開しています。Probe v1.21.0までは組み込みアクションでした。ワークフローが初めて使うときにProbeがダウンロードし、固定したコミットの`action.yml`が示すSHA-256と一致する実行ファイルだけを実行します。Probe v1.21.0以降が必要です。各[リリース](https://github.com/mozership/probe-browser/releases)のノートの先頭に、コピーして使う`uses`の行があります。

このアクションはページが何をするかを判定できないため、[ガード](/ja/guide/concepts/guard)に従いません。`--read-only`や`--allow-host`の下では、`--allow-action`で指定しない限り、このアクションを使うステップを拒否します。

## 基本的な構文

browserステップでは、行う操作を`actions`に順番に並べます。ステップごとに新しいブラウザを起動し、操作を実行して閉じます。そのためクッキーやログイン状態は、そのままでは次のステップに引き継がれません。一連の流れを1つのステップにまとめるか、[ログイン状態の引き継ぎ](#ログイン状態の引き継ぎ)のとおり`save_storage_state`でログイン状態を保存し、次のステップのブラウザをそこから始めます。

```yaml
steps:
  - name: Read the heading
    uses: github.com/mozership/probe-browser@5e0ee79cb660e5c855414935f51ad3eeb7c6ad6e # v0.6.1
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
| `storage_state` | String | なし | `save_storage_state`が書いたファイル。ブラウザはこのファイルのクッキーとlocalStorageを持った状態で始まる。[ログイン状態の引き継ぎ](#ログイン状態の引き継ぎ)を参照 |
| `passkeys` | String | なし | `save_passkeys`が書いたファイル。仮想認証器はこのファイルのパスキーを持った状態で始まる。`virtual_authenticator`がなければ、デフォルトの設定の認証器を使う。[パスキーの引き継ぎ](#パスキーの引き継ぎ)を参照 |
| `virtual_authenticator` | Booleanまたはマップ | なし | 仮想認証器でWebAuthnに応答し、利用者がいなくてもパスキーを登録して使えるようにする。[パスキーでのサインイン](#パスキーでのサインイン)を参照 |

Chromeの起動にも`timeout`の一部を使い、1秒以上かかることもよくあります。短すぎる制限では、最初のページを開く前に時間切れになることがあります。

## アクション

`actions`の各要素は、`name`で操作を指定し、次のフィールドを取ります。

| フィールド | 使うアクション | 説明 |
|-------|---------|-------------|
| `name` | すべて | 下の表にある操作の名前 |
| `id` | 結果を返すアクション | `res.results`や`res.filepaths`で結果を引くキー。省くとアクション名がキーになるため、同じ種類のアクションを後に置くと前の結果を置き換える |
| `url` | `navigate` | 開くアドレス |
| `selector` | 要素に対するアクション | CSSセレクタ。開いたshadow rootの中の要素も探す。[要素の指定](#要素の指定)を参照 |
| `has_text` | 要素に対するアクション | `selector`に一致する要素（`selector`がなければすべての要素）のうち、表示されたテキストがこの値を含む、最も内側の要素を選ぶ。[要素の指定](#要素の指定)を参照 |
| `value` | `type`、`send_keys`、`press`、`set_value`、`select`、`set_user_verified` | 使うテキスト。`press`では`Enter`や`Shift+Tab`のように押すキー。`set_user_verified`では`true`か`false` |
| `attribute` | `get_attribute` | 読む属性を`href`か`[href]`で指定する。リストの場合は最初の要素だけを読む |
| `quality` | `full_screenshot` | `1`から`99`はその品質のJPEGで保存する。それ以外は、省略、`0`、`100`以上、負の数も含めてPNGで保存する |
| `path` | スクリーンショット、`save_storage_state`、`save_passkeys` | スクリーンショットでは非推奨で、画像をこのパスにも書き出す。代わりに`res.filepaths`を使う。`save_storage_state`と`save_passkeys`では、一時ディレクトリに新しいファイルを作る代わりに書き出す場所 |
| `equals` | `wait_text` | テキストがこの値と一致するまで待つ。`equals: ""`は空のテキストになるまで待つ |
| `contains` | `wait_text` | テキストがこの値を含むまで待つ |
| `not_contains` | `wait_text` | テキストがこの値を含まなくなるまで待つ。`Loading`のような仮の表示が内容に置き換わるのを待つときに使う |
| `matches` | `wait_text` | テキストがこの正規表現に一致するまで待つ。正規表現は[Goの構文](https://pkg.go.dev/regexp/syntax)で書く |
| `cookie_name` | `get_cookie` | 読むクッキーの名前 |

`wait_text`には`equals`、`contains`、`not_contains`、`matches`のうち1つまでしか書けず、ほかのアクションには書けません。空の`contains`や`matches`はどのテキストでも通るため、空の`not_contains`はどのテキストも通らないため、これらもエラーになります。これらに反するワークフローは、ブラウザを起動する前にエラーになります。

要素を探すアクションは、要素が現れるまで待ちます。先に`timeout`が尽きるとステップは失敗します。多くは要素が表示されるまでも待ちます。

### 要素の指定

要素に対するアクションは`selector`と`has_text`のどちらか、または両方を取り、それらに一致する最初の要素を操作します。要素はページの順に並び、shadow rootの要素はそのホストのすぐ後に来ます。要素が必要なアクションにどちらもない場合と、要素を操作しないアクションに`has_text`がある場合は、ブラウザを起動する前にエラーになります。

`selector`はCSSセレクタで、文書と、開いたすべてのshadow rootから探します。そのため、コンポーネントがshadow rootに描画した要素も、その外の要素と同じように見つかります。ルートごとに別々に照合するため、`#host p`は`#host`のshadow rootの中の`p`には一致しませんが、`p`は一致します。閉じたshadow rootはページのスクリプトからも、このアクションからも触れられません。`selector`はCSSだけを受け付け、XPathや探すテキストとしては扱いません。

`has_text`は利用者が読むテキストで要素を選ぶため、ステップがページの構造に依存しません。

```yaml
        - name: click
          selector: button
          has_text: Recovery codes
        - name: click
          selector: '[data-slot="dropdown-menu-item"]'
          has_text: Delete label
```

連続する空白と改行を1つの空白とみなしたうえで、表示されたテキストが`has_text`を含む要素を残し、そのうち最も内側の要素だけを選びます。そのため、`has_text`だけを書いても、ボタンを囲むカードではなくボタンが見つかります。コンポーネントのslotに入れた要素は、その要素を表示するslotの中にあるものとして扱います。`<slot>`のように自身の描画領域を持たない要素は、クリックも表示の確認もできないため選びません。大文字と小文字は区別します。非表示の要素には表示されたテキストがないため、表示されてから見つかります。SVGでは描画されるテキストだけが対象で、`<title>`は含みません。

`text`と`wait_text`は、要素の表示されたテキストに加えて、その中にある開いたshadow rootのテキストを、slotが表示するノードをその位置に入れて読みます。`innerText`はどちらも含みません。そのため、shadow rootに描画したメール本文を持つ要素に`wait_text`を使うと本文を読めます。また、`has_text`では、コンポーネントがslotの中身の周りに表示するテキストでそのコンポーネントを探せます。shadow rootのテキストは、`innerText`が文書のテキストを組み立てるのと同じ規則で組み立てます。段落の前後には空行が入り、`<br>`は改行になり、空白はテキストのスタイルに従ってまとめます。

### 移動と待機

ページを開くか、次の操作に必要な状態になるまでステップを待たせます。

| アクション | 内容 |
|--------|--------------|
| `navigate` | `url`を開き、ページの読み込みを待つ。`net::ERR_CONNECTION_REFUSED`のようにブラウザがネットワークのエラーを返すとすぐに失敗する。応答のないアドレスでは`timeout`まで待つ |
| `wait_visible` | `selector`が表示されるまで待つ |
| `wait_not_visible` | `selector`に一致する要素がどれも表示されなくなるまで待つ。条件付きの描画で取り除かれた要素のように、ページからなくなった要素も表示されていないとみなす。最初から存在しない要素も同じなので、要素を表示させる操作の後では、先に`wait_visible`で要素を待つ |
| `wait_enabled` | `selector`が表示され、有効になるまで待つ |
| `wait_ready` | ページの`body`の準備ができるまで待つ。`selector`は使わない |

### ページの読み取り

以下は文字列を`res.results`に、`id`またはアクション名をキーにして入れます。

| アクション | 結果 |
|--------|--------|
| `text` | `selector`の表示されたテキスト。中にある開いたshadow rootのテキストも含む。非表示なら空文字列 |
| `wait_text` | `selector`が表示されるまで待ち、`text`と同じように読んだ表示されたテキスト。`equals`、`contains`、`not_contains`、`matches`を書くと、テキストが期待どおりになるまでも待つ。[テキストを待つ](#テキストを待つ)を参照 |
| `value` | `input`などのフォーム項目の値 |
| `get_attribute` | `selector`の、`attribute`の最初の属性の値。要素がその属性を持たなければ空文字列 |
| `get_html` | 表示された`selector`の外側のHTML |
| `get_cookie` | 開いているページにブラウザが送るクッキー`cookie_name`の値。`HttpOnly`のものも含む。サインインの少し後に設定されるクッキーのように、ページがクッキーを持つまで待つ。先に`timeout`が尽きると、ページが持つクッキーの名前（値は含まない）を示してステップが失敗する。同じ名前のクッキーが2つあれば、ブラウザが先に送る、パスの長いほうの値を返す。パスが同じでドメインだけが違う2つは、ブラウザがどちらを先に送るか決められないため、ステップが失敗する |

### 操作

利用者と同じようにページを操作します。結果は返しません。

| アクション | 内容 |
|--------|--------------|
| `click` | 表示された`selector`をクリックする |
| `double_click` | 表示された`selector`をダブルクリックする |
| `type`、`send_keys` | `selector`が表示されたら、その内容の代わりに`value`を入力する。`input`と`textarea`では先に値を消す。リッチテキストエディタのようなcontenteditableの要素では、Ctrl+Aの後のように内容を選択してから上書きし、エディタが変更を受け取れるようにする。空の`value`は内容を消す。それ以外の要素ではステップが失敗する |
| `press` | `selector`が表示されたら、その要素でキー`value`を押す。`selector`がなければ、フォーカスのある要素で押す。[キーを押す](#キーを押す)を参照 |
| `set_value` | `selector`の`input`、`textarea`、`select`が表示されたら、値を`value`にし、利用者が編集したときと同じく`input`と`change`のイベントを発生させる。入力はしないため、日付はブラウザのロケールによらず`2000-01-02`の形で渡す。日付の`2000-13-01`のように項目が受け付けない値では、ステップが失敗する |
| `submit` | `selector`が属するフォームを送信する |
| `focus` | `selector`にフォーカスを移す |
| `scroll` | `selector`が見える位置までスクロールする |
| `select` | `selector`の`<select>`が表示されたら、値が`value`の選択肢を、なければラベルが`value`の選択肢を選び、利用者が選んだときと同じく`input`と`change`のイベントを発生させる。選択肢がない、選択肢が無効になっている、`<select>`が無効になっている、要素が`<select>`でない場合は、その理由を示してステップが失敗する。囲んでいる`<optgroup>`や`<fieldset>`の`disabled`による無効も含む。失敗のメッセージには選択肢の値が入る。`<select multiple>`では、すでに選ばれている選択肢に加える |
| `hover` | `selector`に`mouseover`イベントを送る。待たず、一致する要素がなければ何もしない |
| `right_click` | `selector`に`contextmenu`イベントを送る。待たず、一致する要素がなければ何もしない |

### スクリーンショット

以下は画像をファイルに保存し、そのパスを`res.filepaths`に、`id`またはアクション名をキーにして入れます。ファイル名の拡張子は形式に合わせて`.png`か`.jpg`になります。

| アクション | 画像 |
|--------|-------|
| `capture_screenshot` | ウィンドウに見えている範囲。PNG |
| `full_screenshot` | ページ全体。PNG。`quality`が`1`から`99`ならJPEG |
| `screenshot` | 表示された`selector`だけ。PNG |

### ストレージの状態

| アクション | 内容 |
|--------|--------------|
| `save_storage_state` | ブラウザのクッキーと、開いているページのオリジンのlocalStorageをJSONファイルに書き出し、そのパスを`res.filepaths`に、`id`またはアクション名をキーにして入れる。ファイルは`path`に書く。`path`がなければ一時ディレクトリに`probe-browser-state-*.json`を新しく作る。[ログイン状態の引き継ぎ](#ログイン状態の引き継ぎ)を参照 |

### パスキー

以下は仮想認証器を使うため、`virtual_authenticator`か`passkeys`が必要です。[パスキーでのサインイン](#パスキーでのサインイン)を参照してください。

| アクション | 内容 |
|--------|--------------|
| `wait_passkey` | 仮想認証器がパスキーを登録するか、パスキーでサインインするまで待ち、どちらを行ったか（`registration`か`authentication`）を`res.results`に、`id`またはアクション名をキーにして入れる。[パスキーを待つ](#パスキーを待つ)を参照 |
| `set_user_verified` | 以後のユーザー検証を、`value: true`なら成功させ、`value: false`なら失敗させる。`user_verification`のある認証器が必要 |
| `save_passkeys` | 仮想認証器が持つパスキーを秘密鍵も含めてJSONファイルに書き出し、そのパスを`res.filepaths`に、`id`またはアクション名をキーにして入れる。ファイルは`path`に書く。`path`がなければ一時ディレクトリに`probe-browser-passkeys-*.json`を新しく作る。`virtual_authenticator`か`passkeys`が必要で、認証器がパスキーを持っていなければ失敗する。[パスキーの引き継ぎ](#パスキーの引き継ぎ)を参照 |

## レスポンスオブジェクト

すべての操作が成功すると、ステップからは次の値を参照できます。

| フィールド | 型 | 説明 |
|-------|------|-------------|
| `res.code` | Integer | `0` |
| `res.results` | Object | 読み取りのアクションが返した値。キーは`id`またはアクション名 |
| `res.filepaths` | Object | スクリーンショット、ストレージの状態、パスキーの保存先。キーは`id`またはアクション名 |
| `res.console_errors` | List | 操作の実行中にページがコンソールに書いたエラー。書いた順に並ぶ。[コンソールの確認](#コンソールの確認)を参照 |
| `rt.duration` | String | ステップにかかった時間。`"663.268333ms"`など |
| `status` | Integer | `0` |

操作が失敗した場合は、テストできるレスポンスはありません。ステップは[エラーハンドリング](#エラーハンドリング)のとおりアクションのエラーとして失敗します。

## 例

どの例も1つのステップです。1つのステップの操作は同じブラウザを共有し、次のステップは新しいブラウザで始まるためです。ただし[ログイン状態の引き継ぎ](#ログイン状態の引き継ぎ)は、ログイン状態を次のステップに渡すため、2つのステップからなります。

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
    uses: github.com/mozership/probe-browser@5e0ee79cb660e5c855414935f51ad3eeb7c6ad6e # v0.6.1
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
    uses: github.com/mozership/probe-browser@5e0ee79cb660e5c855414935f51ad3eeb7c6ad6e # v0.6.1
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

### テキストを待つ

データを取得して表示するページでは、読み込みから少し遅れてデータが表示されます。そのため、要素が表示された時点で`text`で読むと`Loading`が返り、テストが失敗することがあります。`wait_text`に`equals`、`contains`、`not_contains`、`matches`を書くと、テキストが期待どおりになるまで読み直します。モデルが書く要約のように、表示されれば何が入ってもよいテキストでは、`not_contains`で仮の表示が消えるのを待ちます。

```yaml
  - name: Order list
    uses: github.com/mozership/probe-browser@5e0ee79cb660e5c855414935f51ad3eeb7c6ad6e # v0.6.1
    with:
      timeout: 30s
      actions:
        - name: navigate
          url: "{{vars.url}}/orders"
        - name: wait_text
          id: status
          selector: "#status"
          matches: '^\d+ orders$'
    test: res.code == 0
    outputs:
      status: res.results.status
```

先にステップの`timeout`が尽きると、最後に読んだテキストを示すアクションのエラーとしてステップが失敗し、[失敗時のページ](#失敗時のページ)のとおりページを保存します。

```
wait_text "#status": the text was "Loading", want it to match "^\\d+ orders$": context deadline exceeded (page at failure: ...)
```

### キーを押す

`press`のキーは、`Enter`、`Tab`、`Escape`、`Backspace`、`ArrowDown`のように`KeyboardEvent.key`と同じ名前で書くか、`a`のように1文字で書きます。`Space`は`" "`として扱います。修飾キー（`Alt`、`Control`、`Meta`、`Shift`）は、`Shift+Tab`や`Control+a`のようにキーの前に`+`でつないで書きます。知らないキーは、ブラウザを起動する前にエラーになります。

クリックするボタンがなく、Enterで送信する検索欄の例です。

```yaml
  - name: Search
    uses: github.com/mozership/probe-browser@5e0ee79cb660e5c855414935f51ad3eeb7c6ad6e # v0.6.1
    with:
      timeout: 30s
      actions:
        - name: navigate
          url: "{{vars.url}}/"
        - name: type
          selector: "#search"
          value: probe
        - name: press
          value: Enter
        - name: wait_text
          id: heading
          selector: h1
          contains: probe
    test: res.code == 0
```

`value`が`"\r"`で終わる`type`もEnterを押しますが、YAMLのダブルクォートの文字列の場合に限ります。クォートなしやシングルクォートでは、`\r`は2文字として入力されます。

### クッキーを読む

サインインするステップは、アプリがクッキーに持つトークンを`get_cookie`で読み、そのユーザーとしてアプリのAPIを呼ぶときのように、後のステップに渡せます。

```yaml
  - name: Sign up
    id: signup
    uses: github.com/mozership/probe-browser@5e0ee79cb660e5c855414935f51ad3eeb7c6ad6e # v0.6.1
    with:
      timeout: 30s
      actions:
        - name: navigate
          url: "{{vars.url}}/signup"
        # ... the actions that sign up
        - name: get_cookie
          id: token
          cookie_name: access_token
    test: res.code == 0
    outputs:
      token: res.results.token
```

### ログイン状態の引き継ぎ

ログインするステップで`save_storage_state`を使ってログイン状態を保存し、後のステップで`storage_state`にそのファイルを指定すると、後のステップのブラウザはログインした状態で始まります。ステップごとにログインし直す必要はありません。

```yaml
  - name: Sign in
    id: login
    uses: github.com/mozership/probe-browser@5e0ee79cb660e5c855414935f51ad3eeb7c6ad6e # v0.6.1
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
        - name: save_storage_state
          id: state
    test: res.code == 0
    outputs:
      state: res.filepaths.state

  - name: Orders
    uses: github.com/mozership/probe-browser@5e0ee79cb660e5c855414935f51ad3eeb7c6ad6e # v0.6.1
    with:
      storage_state: "{{outputs.login.state}}"
      actions:
        - name: navigate
          url: "{{vars.url}}/orders"
        - name: wait_text
          selector: "h1"
          equals: "Orders"
    test: res.code == 0
```

ファイルには、ブラウザが持つすべてのサイトのクッキー（`HttpOnly`のものも含む）と、保存したときに開いていたページのオリジンのlocalStorageが入ります。別のオリジンのlocalStorageも残すには、そのオリジンのページを開いた状態で保存します。ファイルは[Playwrightのstorage state](https://playwright.dev/docs/auth)と同じ形式なので、どちらのツールで書いたファイルももう一方で読めます。

`storage_state`を指定したブラウザは、最初の操作の前にクッキーを読み込みます。localStorageは、項目のあるオリジンごとにそのオリジンのページを開いて書き込みます。このページへのリクエストにはブラウザの中で空のページを返すため、サーバーには届かず、アプリのスクリプトも動きません。最初の操作が始まるときには、ブラウザは`about:blank`に戻っています。ファイルがない場合や、`cookies`と`origins`のリスト（空でもよい）を持つstorage stateでない場合は、ブラウザを起動する前にステップが失敗します。ファイルを書くステップが失敗していた場合や、`save_passkeys`が書くファイルを代わりに渡した場合がこれに当たります。エラーには、何が足りないかが出ます。

```
storage_state: /tmp/probe-browser-passkeys-123.json is not a storage state: it has no list of cookies
```

クッキーを使えばそのユーザーとしてログインできるため、ファイルはProbeを実行したユーザーだけが読めるようにします。`path`を指定しなければ一時ディレクトリに書くため、リポジトリには入りません。`path`を指定する場合は、バージョン管理の対象と、CIでアップロードするファイルから外してください。

### パスキーでのサインイン

`virtual_authenticator`を指定すると、ブラウザはTouch IDやセキュリティキーのようなプラットフォームの認証器の代わりに、仮想認証器でWebAuthnに応答します。センサーに触れる人がいなくても、ページはパスキーを登録し、それでサインインできます。`true`では、パスキーを保持でき、ユーザー検証に成功する認証器を使います。

```yaml
  - name: Register a passkey and sign in with it
    uses: github.com/mozership/probe-browser@5e0ee79cb660e5c855414935f51ad3eeb7c6ad6e # v0.6.1
    with:
      timeout: 30s
      virtual_authenticator: true
      actions:
        - name: navigate
          url: "{{vars.url}}/account/passkeys"
        - name: click
          selector: "#add-passkey"
        - name: wait_visible
          selector: ".passkey-added"
        - name: navigate
          url: "{{vars.url}}/logout"
        - name: navigate
          url: "{{vars.url}}/login"
        - name: click
          selector: "#sign-in-with-passkey"
        - name: wait_visible
          selector: "#dashboard"
    test: res.code == 0
```

`true`の代わりにマップを書くと、書いたオプションだけを変え、ほかはデフォルトのままにします。

| オプション | デフォルト | 説明 |
|--------|---------|-------------|
| `transport` | `internal` | 認証器への接続方法。端末に内蔵された認証器なら`internal`、ほかに`usb`、`nfc`、`ble`、`hybrid` |
| `resident_key` | `true` | パスキーである、発見可能な資格情報を保持できるか |
| `user_verification` | `true` | 指紋やPINなどでユーザーを検証できるか |
| `user_verified` | `user_verification`と同じ | ユーザーの検証が成功するか。`false`にすると、検証が拒否されたときのページの動きを確かめられる |
| `backup_eligible` | `false` | パスワードマネージャーの資格情報のように、資格情報がバックアップ可能だと示すか |
| `backed_up` | `false` | 資格情報がバックアップ済みだと示すか。`backup_eligible`が必要 |

```yaml
      virtual_authenticator:
        user_verified: false
```

知らないオプションや受け付けない値は、ブラウザを起動する前にステップを失敗させます。

認証器はCTAP2で通信し、利用者が触れたときと同じようにすぐに応答します。認証器はページのタブに属するため、ページが開くポップアップには認証器がありません。WebAuthnは安全なコンテキストでしか動かないため、ページは`https`か`localhost`で開く必要があります。認証器とそのパスキーは、ブラウザのほかの状態と同じくステップの終わりに消えます。後のステップでパスキーを使ってサインインするには、[パスキーの引き継ぎ](#パスキーの引き継ぎ)のとおり保存します。

#### パスキーを待つ

パスキーを登録するページは、新しい資格情報を先にサーバーに送るため、登録したと表示するまでに少し時間がかかることがあります。`wait_passkey`は認証器そのものの動作を待ち、何を行ったかを返します。

```yaml
  - name: Register a passkey
    uses: github.com/mozership/probe-browser@5e0ee79cb660e5c855414935f51ad3eeb7c6ad6e # v0.6.1
    with:
      timeout: 30s
      virtual_authenticator: true
      actions:
        - name: navigate
          url: "{{vars.url}}/account/passkeys"
        - name: click
          selector: "#add-passkey"
        - name: wait_passkey
          id: added
        - name: save_passkeys
    test: res.code == 0 && res.results.added == "registration"
```

認証器の動作はステップの始めから記録します。各`wait_passkey`は、それより前の`wait_passkey`が受け取っていない最も早い動作を受け取るため、`wait_passkey`が始まる前に終わった登録も取りこぼしません。`passkeys`で渡したパスキーは登録には数えません。先にステップの`timeout`が尽きると、ステップは失敗します。

#### ユーザー検証を失敗させる

`set_user_verified`は、指紋が一致しなくなったときのように、以後の操作で認証器がユーザーの検証に成功するかを変えます。1つのステップでパスキーを登録し、続けて検証に失敗するサインインをページがどう扱うかを確かめられます。

```yaml
      virtual_authenticator: true
      actions:
        # ... register a passkey ...
        - name: set_user_verified
          value: false
        - name: click
          selector: "#sign-in-with-passkey"
        - name: wait_visible
          selector: ".passkey-error"
```

最初から失敗させるには、代わりに`virtual_authenticator`に`user_verified: false`を書きます。

### パスキーの引き継ぎ

パスキーを登録するステップで`save_passkeys`を使ってパスキーを保存し、後のステップで`passkeys`にそのファイルを指定すると、後のステップの認証器はそのパスキーを持った状態で始まります。

```yaml
  - name: Register a passkey
    id: register
    uses: github.com/mozership/probe-browser@5e0ee79cb660e5c855414935f51ad3eeb7c6ad6e # v0.6.1
    with:
      timeout: 30s
      virtual_authenticator: true
      actions:
        - name: navigate
          url: "{{vars.url}}/account/passkeys"
        - name: click
          selector: "#add-passkey"
        - name: wait_visible
          selector: ".passkey-added"
        - name: save_passkeys
          id: passkeys
    test: res.code == 0
    outputs:
      passkeys: res.filepaths.passkeys

  - name: Sign in with it
    uses: github.com/mozership/probe-browser@5e0ee79cb660e5c855414935f51ad3eeb7c6ad6e # v0.6.1
    with:
      timeout: 30s
      passkeys: "{{outputs.register.passkeys}}"
      actions:
        - name: navigate
          url: "{{vars.url}}/login"
        - name: click
          selector: "#sign-in-with-passkey"
        - name: wait_visible
          selector: "#dashboard"
    test: res.code == 0
```

`passkeys`は、デフォルトの設定か、`virtual_authenticator`のオプションの認証器を使います。`virtual_authenticator: false`と一緒に書くとステップが失敗します。`save_passkeys`は認証器がまだパスキーを持っていないと失敗するため、ページがパスキーの登録を表示するのを待ってから使います。

ファイルは、リライングパーティが保持するのと同じくバイト列をbase64urlで表し、パスキーを`passkeys`の下に並べます。

```json
{
  "passkeys": [
    {
      "credentialId": "Wq2b...",
      "rpId": "example.com",
      "userHandle": "dXNlci03",
      "userName": "user-7",
      "privateKey": "MIGHAgEA...",
      "signCount": 1,
      "isResidentCredential": true,
      "backupEligibility": false,
      "backupState": false
    }
  ]
}
```

ファイルは手で書くこともできます。定期的な確認で、実行のたびにパスキーを登録する代わりに、テスト用のアカウントに一度登録したパスキーでサインインするときに使います。`credentialId`、`rpId`、`privateKey`（PKCS#8のECDSA P-256の鍵）は必須で、`isResidentCredential`は省くと`true`になります。ファイルがない場合や、認証器が使えないパスキーがある場合は、ブラウザを起動する前にステップが失敗します。パスキーは、その`rpId`のページにだけ提示します。

`signCount`はパスキーの署名カウンタの初期値です。リライングパーティは、複製された鍵の兆候として、カウンタが前回より大きくないサインインを拒否することがあります。そのため、同じファイルを繰り返し使うときは、サインインのたびに`save_passkeys`でカウンタを進めるか、`"signCount": -1`でカウンタを持たないパスキーにします。

秘密鍵を使えばそのユーザーとしてサインインできるため、ファイルはProbeを実行したユーザーだけが読めるようにします。バージョン管理の対象と、CIでアップロードするファイルから外し、手で書いたファイルはシークレットに入れてください。

### コンソールの確認

クリックの処理で例外が起きるなど、ページは表示に出ないところで壊れていることがあります。`res.console_errors`には、操作の実行中にページがコンソールにエラーとして書いたものが入ります。`console.error`の呼び出し、失敗した`console.assert`、捕まえられなかった例外と、処理されなかったPromiseのrejectです。各要素は次のフィールドを持ちます。

| フィールド | 説明 |
|-------|-------------|
| `type` | `error`、`assert`、`exception`のいずれか |
| `text` | DevToolsのコンソールに表示される内容。例外では`Uncaught`で始まり、Promiseのrejectでは`Uncaught (in promise)`で始まる |
| `url`、`line`、`column` | エラーが発生した場所。行と列はDevToolsと同じく1から数える。ブラウザが場所を返さない場合は空と`0` |

画像の読み込みの失敗のようなブラウザ自身のメッセージや、ログと警告は入りません。エラーはページのタブから読むため、ポップアップや、別のサイトのiframeのエラーも入りません。

エラーが1つでもあればステップを失敗させるには、リストが空であることをテストします。

```yaml
  - name: Checkout page
    uses: github.com/mozership/probe-browser@5e0ee79cb660e5c855414935f51ad3eeb7c6ad6e # v0.6.1
    with:
      actions:
        - name: navigate
          url: "{{vars.url}}/checkout"
        - name: click
          selector: "#pay"
        - name: wait_visible
          selector: "#receipt"
    test: res.code == 0 && len(res.console_errors) == 0
    echo: "{{res.console_errors}}"
```

操作が失敗したときは、失敗のメッセージに最初のいくつかのエラーが入ります。操作が失敗した原因がそのエラーであることが多いためです。

```
... context deadline exceeded (console errors: "Uncaught TypeError: Cannot read properties of null (reading 'total') (http://localhost:8090/checkout.js:42:17)") (page at failure: ...)
```

### スクリーンショットを残す

保存した画像のパスは`res.filepaths`に入り、表示したり次に渡したりできます。

```yaml
  - name: Checkout page
    uses: github.com/mozership/probe-browser@5e0ee79cb660e5c855414935f51ad3eeb7c6ad6e # v0.6.1
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

アクションが完了できないと、ステップ全体がアクションのエラーとして失敗し、実行は終了ステータス`3`で終わります。セレクタが何にも一致しなくても空の結果にはなりません。`wait_visible`、`text`、`click`など要素を探すアクションは、ステップの`timeout`まで待ち続けます。ナビゲーションは、接続の拒否のようにブラウザがネットワークのエラーを返すとすぐに失敗しますが、応答のないアドレスでは`timeout`まで待ちます。いずれの場合もテストできる`res`はなく、失敗のメッセージにはブラウザのエラーと、[コンソールの確認](#コンソールの確認)のとおりページがコンソールに書いたエラーが入ります。

### 失敗時のページ

アクションが失敗すると、Probeはその時点のページを保存します。ページ全体のPNGのスクリーンショットと、文書のHTMLです。2つのファイルには`probe-browser-failure-1947177907.png`と`.html`のように同じ名前が付きます。失敗のメッセージには、ページのURLとあわせて両方のファイルのパスが入ります。

```
... context deadline exceeded (page at failure: url http://localhost:8090/, screenshot /tmp/probe-browser-failure-1947177907.png, html /tmp/probe-browser-failure-1947177907.html)
```

`--report`で書き出すファイルでも、このメッセージが失敗の`message`になります。テストがfalseになるのはアクションの失敗ではないため、そのときはページを保存しません。テキストが期待どおりでないときに保存させるには、`test`で確かめる代わりに[`wait_text`](#テキストを待つ)に`equals`、`contains`、`not_contains`、`matches`を書きます。どんな場合でもページを残すには`capture_screenshot`アクションを加えます。

ファイルはシステムの一時ディレクトリに保存します。`evidence_dir`を指定するとそこに保存し、ディレクトリは必要に応じて作成します。CIのアーティファクトとしてアップロードする場合に便利です。ページには個人の情報が写りうるため、ファイルはProbeを実行したユーザーだけが読めるようにします。

```yaml
- name: Checkout page
  uses: github.com/mozership/probe-browser@5e0ee79cb660e5c855414935f51ad3eeb7c6ad6e # v0.6.1
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
