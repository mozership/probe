# IMAP Action

IMAPアクションは、IMAPサーバーに接続してメールの読み取り、検索、メールボックス管理などのメール操作を実行します。

## 基本構文

IMAPのステップでは接続を確立し、`commands`に並べたコマンドを順に実行します。

```yaml
vars:
  imap_username: "{{IMAP_USERNAME}}"
  imap_password: "{{IMAP_PASSWORD}}"

steps:
  - name: "メールチェック"
    uses: imap
    with:
      host: "imap.example.com"
      port: 993
      username: "{{vars.imap_username}}"
      password: "{{vars.imap_password}}"
      tls: true
      commands:
      - name: "select"
        mailbox: "INBOX"
      - name: "search"
        criteria:
          not_flags: ["seen"]
    test: res.code == 0
```

## パラメータ

パラメータでは、接続先のサーバー、提示する認証情報、接続の保護方法、そして接続後に実行するIMAPコマンドの一覧を指定します。

### `host` (必須)

**型:** String  
**説明:** IMAPサーバーのホスト名またはIPアドレス  
**テンプレート式をサポート:** はい

```yaml
with:
  host: "imap.gmail.com"
  host: "imap.example.com" 
  host: "{{vars.imap_server}}"
```

### `port` (オプション)

**型:** Integer  
**デフォルト:** `993`  
**説明:** IMAPサーバーのポート

```yaml
with:
  port: 993   # IMAPS (SSL/TLS)
  port: 143   # TLSなしのIMAP (tls: false)
```

### `username` (必須)

**型:** String  
**説明:** IMAP認証のユーザー名  
**テンプレート式をサポート:** はい

```yaml
vars:
  email_user: "{{EMAIL_USER}}"

with:
  username: "{{vars.email_user}}"
  username: "user@example.com"
```

### `password` (必須)

**型:** String  
**説明:** IMAP認証のパスワード  
**テンプレート式をサポート:** はい

```yaml
vars:
  email_password: "{{EMAIL_PASSWORD}}"
  app_password: "{{EMAIL_APP_PASSWORD}}"

with:
  password: "{{vars.email_password}}"
  password: "{{vars.app_password}}"
```

### `tls` (オプション)

**型:** Boolean
**デフォルト:** `true`
**説明:** TLS/SSLで暗号化して接続するかどうか

```yaml
with:
  host: "imap.example.com"
  port: 993
  tls: true     # TLSを使う（推奨）
```

STARTTLSには対応していません。`tls: false`では、パスワードを含むセッション全体が暗号化されずに送られます。

### `insecure_skip_tls` (オプション)

**型:** Boolean
**デフォルト:** `false`
**説明:** サーバーの証明書を検証せずに受け入れる

`tls: true`では証明書をシステムが信頼する認証局で検証します。そのため、ローカルやステージングのメールサーバーのように自己署名証明書を使うサーバーでは`x509: certificate signed by unknown authority`で失敗します。`insecure_skip_tls: true`にすると、それでも接続します。通信は暗号化されたままですが、なりすましへの防御はなくなるため、自分で管理するサーバーにだけ使います。

```yaml
with:
  host: "mail.staging.internal"
  port: 993
  tls: true
  insecure_skip_tls: true
```

### `timeout` (オプション)

**型:** 期間の文字列、または秒数
**デフォルト:** `30s`
**説明:** セッション全体の制限時間。接続、TLSのハンドシェイク、ログイン、すべてのコマンド、ログアウトを含む

```yaml
with:
  timeout: "60s"   # または 60
```

時間内に応答しないサーバーとの接続は打ち切られ、ステップはアクションのエラーとして失敗し、実行は終了ステータス`3`で終わります。メッセージは`IMAP session timed out after 30s while running commands`のようになります。サーバーがコマンドを拒否した場合は`res.code`が`1`になるのと異なります。期間でも数値でもない値や、0以下の値はエラーになります。

### `commands` (オプション)

**型:** コマンドオブジェクトの配列  
**説明:** 順次実行するIMAPコマンド。省略すると、接続してログインし、ログアウトするだけなので、ログインだけを確かめられます

コマンドは1つのセッションの中で順に実行されます。失敗したコマンドがあるとそこで止まり、`res.code`が`1`になり、`res.error`にそのコマンドが示されます。`res.data`には、それより前のコマンドの結果が残ります。

コマンドオブジェクトは以下のフィールドを取ります。どのコマンドがどのフィールドを読むかは、各コマンドの説明に示します。

| フィールド | 使うコマンド |
|-----------|-------------|
| `name` | すべてのコマンド（必須）。実行するコマンドで、大文字と小文字は区別しない |
| `mailbox` | `select`、`examine`、`copy`、`uid copy`、`create`、`delete`、`subscribe`、`unsubscribe` |
| `oldmailbox`、`newmailbox` | `rename` |
| `reference`、`pattern` | `list` |
| `criteria` | `search`、`uid search` |
| `sequence` | `fetch`、`uid fetch`、`store`、`uid store`、`copy`、`uid copy` |
| `dataitem` | `fetch`、`uid fetch`、`store`、`uid store` |
| `value` | `store`、`uid store` |

```yaml
with:
  commands:
  - name: "select"
    mailbox: "INBOX"
  - name: "search"
    criteria:
      since: "today"
  - name: "fetch"
    sequence: "1:5"
    dataitem: "ALL"
```

## IMAPコマンド

`commands`の各要素は1つのIMAP操作を表し、その操作に必要なフィールドを持ちます。ここでは対応しているコマンドと、検索で指定できる条件を示します。

### サポートされているコマンド

- **select**: 読み書き操作用のメールボックス選択
- **examine**: 読み取り専用でのメールボックス選択  
- **search**: 条件によるメッセージ検索
- **uid search**: UIDを使用したメッセージ検索
- **list**: 使用可能なメールボックスの一覧表示
- **fetch**: メッセージデータの取得
- **uid fetch**: UIDを使用したメッセージデータの取得
- **store**: メッセージのフラグの変更
- **uid store**: UIDを使用したメッセージのフラグの変更
- **copy**: メッセージを別のメールボックスへコピー
- **uid copy**: UIDを使用したメッセージのコピー
- **create**: メールボックスの作成
- **delete**: メールボックスの削除
- **rename**: メールボックス名の変更
- **subscribe**: メールボックスの購読
- **unsubscribe**: メールボックスの購読解除
- **noop**: 操作なし（キープアライブ）

#### fetchとuid fetch

メッセージを、`fetch`はシーケンス番号で、`uid fetch`はUIDで取得します。`sequence`を省くと、最新の検索で見つかったメッセージを取得します。ただし種類が合っている場合に限り、`fetch`には`search`、`uid fetch`には`uid search`が必要です。`fetch`には`dataitem`が必要で、`uid fetch`は省くと`ALL`を取得します。どちらも結果を`res.data.fetch`に入れます。

```yaml
- name: "fetch"
  sequence: "1:5"       # シーケンス範囲。省くと直前の検索結果
  dataitem: "ALL"       # 取得するデータ項目
- name: "fetch"
  sequence: "*"         # 最新のメッセージ
  dataitem: "ENVELOPE FLAGS"
- name: "uid search"
  criteria:
    not_flags: ["seen"]
- name: "uid fetch"     # 直前に見つかったUIDをALLで取得
```

`dataitem`には、マクロの`ALL`、`FAST`、`FULL`のいずれかを単独で書くか、`ENVELOPE`、`FLAGS`、`INTERNALDATE`、`RFC822`、`RFC822.HEADER`、`RFC822.SIZE`、`RFC822.TEXT`、`UID`、`BODYSTRUCTURE`と、`BODY[...]`か`BODY.PEEK[...]`のセクションを空白で区切って並べます。大文字と小文字は区別せず、`(FLAGS UID)`のように括弧で囲んでも構いません。セクションの後ろには、先頭512バイトを取る`BODY.PEEK[TEXT]<0.512>`のように部分範囲を付けられます。これら以外の項目を書くとコマンドは失敗します。`ENVELOPE`は`from`、`to`、`subject`を、`RFC822.SIZE`は`size`を埋めます。IMAPと同じく、`RFC822`は`BODY[]`、`RFC822.HEADER`は`BODY.PEEK[HEADER]`、`RFC822.TEXT`は`BODY[TEXT]`です。セクションはメッセージの以下のフィールドを埋めます。

| セクション | 埋めるフィールド |
|-----------|----------------|
| `BODY[]` | `body`にヘッダーを含むメッセージ全体 |
| `BODY[TEXT]` | `body`に本文だけ |
| `BODY[HEADER]` | `headers`にすべてのヘッダーフィールド |
| `BODY[HEADER.FIELDS (SUBJECT FROM)]` | `headers`に指定したフィールド |

`BODY[...]`で取得したメッセージは既読になり、`BODY.PEEK[...]`ではフラグは変わりません。`headers`のヘッダー名は`headers.subject`のように小文字です。本文に`<html`が含まれていれば、`html_body`にも入ります。

#### create、delete、rename、subscribe、unsubscribe

メールボックスの作成、削除、名前の変更、購読への追加と解除を行います。`rename`は現在の名前を`oldmailbox`に、新しい名前を`newmailbox`に取り、ほかのコマンドは`mailbox`を取ります。名前がないとコマンドは失敗します。

```yaml
- name: "create"
  mailbox: "Work"
- name: "rename"
  oldmailbox: "Work"
  newmailbox: "Projects"
- name: "subscribe"
  mailbox: "Projects"
- name: "unsubscribe"
  mailbox: "Projects"
- name: "delete"
  mailbox: "Projects"
```

#### noop

サーバーに何も依頼しません。セッションがまだ生きていることを確かめ、選択中のメールボックスの変化をサーバーに報告させます。

```yaml
- name: "noop"
```

#### storeとuid store

メッセージのフラグを設定、追加、削除します。`dataitem`は、置き換えるなら`FLAGS`、追加するなら`+FLAGS`、削除するなら`-FLAGS`です。末尾に`.SILENT`を付けると、サーバーは新しいフラグを返しません。`value`にはフラグを括弧付きか括弧なしで並べます。`FLAGS`に空の値か`()`を指定するとフラグをすべて消し、片方だけの括弧はエラーになります。`uid store`は`sequence`にUIDを取ります。`sequence`を省くと、`fetch`と同じく最新の検索の結果を対象にします。ただし種類が合っている場合に限ります。`store`には`search`、`uid store`には`uid search`が必要です。検索のたびに、種類を問わずそれ以前の検索結果は置き換わるため、古い検索が使われることはありません。

`sequence`の中で単独の`*`は最後のメッセージを指し、数値は`1`から`4294967295`でなければなりません。`0`やそれより大きい値は`*`とは解釈せずエラーにします。番号はメールボックスごとのものなので、`select`や`examine`を実行すると直前の検索結果は使われなくなります。メールボックスを切り替えたあとに`sequence`を省いたコマンドは、別のメッセージを対象にせずエラーになります。

```yaml
- name: "select"
  mailbox: "INBOX"
- name: "store"
  sequence: "1:3"
  dataitem: "+FLAGS"
  value: '\Seen \Flagged'
```

`res.data.store.count`は、サーバーが新しいフラグとともに返したメッセージの数で、`.SILENT`では`0`です。変更できるのはフラグだけで、ほかのデータ項目を指定するとコマンドは失敗します。

#### copyとuid copy

メッセージを、既にある別のメールボックスへコピーします。`uid copy`は`sequence`にUIDを取り、`sequence`を省くとどちらも直前の検索結果を対象にします。

```yaml
- name: "select"
  mailbox: "INBOX"
- name: "copy"
  sequence: "1:2"
  mailbox: "Archive"
```

`res.data.copy.count`は、サーバーが報告したときにコピーされたメッセージの数です。サーバーがUIDPLUSに対応していれば報告され、そうでなければ`0`です。

存在しないメールボックスへの`copy`のようにコマンドが失敗すると、ほかのIMAPコマンドと同じく`res.code`が`1`になり、`res.error`に理由が入ります。

### 検索条件

`search`と`uid search`の絞り込みは`criteria`で指定します。`search`はシーケンス番号を、`uid search`はUIDを返し、どちらも結果を`res.data.search`に入れるため、後の検索が前の検索の結果を置き換えます。この結果は、`sequence`を省いた後続の`fetch`、`store`、`copy`の対象にもなります。

```yaml
criteria:
  since: "today"              # 今日以降に届いた
  not_flags: ["seen"]         # 未読
  headers:                    # ヘッダーフィールドがテキストを含む
    from: "sender@example.com"
    subject: "件名"
  bodies: ["重要"]            # 本文がテキストを含む
  texts: ["会議"]             # ヘッダーか本文がテキストを含む
```

指定した条件はすべて満たす必要があります。条件は以下のとおりです。

| 条件 | 型 | 一致するメッセージ |
|------|----|------------------|
| `seq_nums` | 文字列の配列 | シーケンス番号が`"2:3"`、`"5"`、`"10:*"`などに含まれる |
| `uids` | 文字列の配列 | UIDが同じ形式の範囲に含まれる |
| `since` | 日付 | その日以降に届いた |
| `before` | 日付 | その日より前に届いた |
| `sent_since` | 日付 | `Date`ヘッダーがその日以降 |
| `sent_before` | 日付 | `Date`ヘッダーがその日より前 |
| `headers` | Object | キーのヘッダーフィールドが値を含む |
| `bodies` | 文字列の配列 | 本文がすべての文字列を含む |
| `texts` | 文字列の配列 | ヘッダーか本文がすべての文字列を含む |
| `flags` | 文字列の配列 | すべてのフラグを持つ |
| `not_flags` | 文字列の配列 | どのフラグも持たない |

フラグは`seen`、`answered`、`flagged`、`deleted`、`draft`、`recent`のようにバックスラッシュなしで書くか、`'\Seen'`のようにバックスラッシュ付きで書きます。それ以外の名前は`$Important`のようなキーワードとして、そのまま渡します。`unseen`というフラグはないため、未読のメッセージは`not_flags: ["seen"]`で指定します。

日付には以下のいずれかを書きます。

- `today`または`yesterday`。probeを実行しているマシンのローカルタイムゾーンの0時から
- `2 hours ago`のような`N hours ago`または`N minutes ago`
- `2006-01-02`、`2006/01/02`、`02/01/2006`（日が先）、`02-Jan-2006`
- `2006-01-02T15:04:05+09:00`のようなRFC 3339、または`02 Jan 06 15:04 JST`のようなRFC 822

IMAPは時刻を除いた日付で比較するため、日付のうち日だけが意味を持ちます。`2 hours ago`は、その日の始まりからのメッセージに一致します。

一致するメッセージがなければ、`count`は`0`、`all`は空になります。

## レスポンスオブジェクト

IMAPアクションは以下の構造を持つ`res`オブジェクトを提供します：

| プロパティ | 型 | 説明 |
|----------|------|-------------|
| `code` | Integer | すべてのコマンドが成功すれば`0`、失敗したコマンドがあれば`1`、コマンドは成功したがログアウトに失敗すれば`2` |
| `data` | Object | コマンドタイプ別に整理されたコマンド結果 |
| `error` | String | 操作が失敗した場合のエラーメッセージ |

トップレベルの`status`は`res.code`と同じ値です。`rt.duration`（例: `"12ms"`）と`rt.sec`には、接続からログアウトまでのセッション全体にかかった時間が入ります。接続、ログイン、タイムアウトの失敗は`res.code`にはならず、アクションのエラーとしてステップが失敗します。

`res.data`には、コマンドごとにその名前のエントリーがあります。実行されなかったコマンドのエントリーはゼロ値のままです。`success`が`false`になるのはこのときだけで、失敗したコマンドは`res.code`を`1`にして実行を止めます。`uid search`、`uid fetch`、`uid store`、`uid copy`は、`uid`の付かないコマンドのエントリーを共有します。

| エントリー | フィールド |
|-----------|-----------|
| `select`、`examine` | `exists`（メールボックス内のメッセージ数）、`recent`、`first_unseen`（最初の未読メッセージのシーケンス番号。サーバーが示さなければ`0`）、`uid_next`、`flags`、`permanent_flags` |
| `search` | `all`（`2:3`のような集合で表した一致した番号）、`min`、`max`、`count` |
| `list` | `mailboxes`（それぞれ`name`、`attributes`、`delimiter`を持つ）、`count` |
| `fetch` | `messages`、`count` |
| `store`、`copy` | `success`、`count` |
| `create`、`delete`、`subscribe`、`unsubscribe` | `success`、`mailbox` |
| `rename` | `success`、`old_mailbox`、`new_mailbox` |
| `noop` | `success` |

`res.data.fetch.messages`の各要素は`uid`、`flags`、`date`、`from`、`to`、`subject`、`size`、`body`、`html_body`、`headers`を持ち、取得したデータ項目に応じて埋まります。`date`はエンベロープの`Date`をRFC 3339で表したもので（例: `2025-10-08T07:11:55Z`）、メッセージにない場合は空です。`from`と`to`は最初のアドレスだけです。メッセージには`res.data.fetch.messages[0].from`のように添字でアクセスします。

## 使用例

ポート、TLS、認証情報はプロバイダによって異なります。以下ではGmailへの接続設定と、受信を監視するワークフローを示します。

### Gmail設定

Gmailではアカウントのパスワードではなくアプリパスワードを使い、993番ポートにTLSで接続します。

```yaml
vars:
  gmail_username: "{{GMAIL_USERNAME}}"
  gmail_app_password: "{{GMAIL_APP_PASSWORD}}"

steps:
  - name: "Gmailの受信箱をチェック"
    uses: imap
    with:
      host: "imap.gmail.com"
      port: 993
      username: "{{vars.gmail_username}}"
      password: "{{vars.gmail_app_password}}"  # アプリパスワードを使用
      tls: true
      commands:
      - name: "select"
        mailbox: "INBOX"
      - name: "search"
        criteria:
          not_flags: ["seen"]
          since: "today"
      - name: "fetch"
        sequence: "*"
        dataitem: "ENVELOPE FLAGS"
    test: res.code == 0
    outputs:
      unread_count: res.data.search.count
      latest_sender: res.data.fetch.messages[0].from
```

### メール監視ワークフロー

定期的に受信箱を検索すれば、届くはずのメールが届いているかを監視できます。

```yaml
name: メールアラート監視
vars:
  monitor_email: "{{MONITOR_EMAIL}}"
  monitor_password: "{{MONITOR_PASSWORD}}"

jobs:
- name: 重要なアラートを監視
  repeat:
    count: 288     # 24時間、5分間隔で実行
    interval: "5m"
  steps:
  - name: "重要なアラートをチェック"
    id: check-alerts
    uses: imap
    with:
      host: "imap.example.com"
      username: "{{vars.monitor_email}}"
      password: "{{vars.monitor_password}}"
      commands:
      - name: "select"
        mailbox: "INBOX"
      - name: "search"
        criteria:
          headers:
            subject: "重要"
          not_flags: ["seen"]
          since: "today"
    test: res.code == 0
    outputs:
      critical_count: res.data.search.count
      has_critical: res.data.search.count > 0
```

## セキュリティに関する考慮事項

IMAPアクションは以下のセキュリティ対策を実装しています：

- **TLS暗号化**: データ保護のためデフォルトでTLSを使用
- **証明書検証**: デフォルトでサーバー証明書を検証
- **認証情報の保護**: パスワードはログと出力でマスク化
- **接続タイムアウト**: ハング状態の接続を防止
- **認証**: 標準的なIMAP認証方式をサポート
