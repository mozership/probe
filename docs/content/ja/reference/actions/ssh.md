# SSH Action

SSH Actionを使用すると、リモートサーバーにSSH接続してコマンドを実行できます。デプロイメント、リモート監視、サーバー管理などの用途に最適です。

## 基本的な使用方法

SSHのステップには、接続先のホスト、ユーザー、認証方法、実行するコマンドが必要です。

```yaml
- name: サーバー状況を確認
  uses: ssh
  with:
    host: "example.com"
    user: "deploy"
    password: "your_password"
    cmd: "uptime && df -h"
  test: res.code == 0
```

## パラメータ

パラメータは4つに分かれます。どのステップでも必要なもの、少なくとも1つを指定する認証用のもの、接続を調整するオプション、そして値を環境変数から与えるためのものです。

### 必須パラメータ

以下の3つには既定値がなく、どのステップでも指定します。

| パラメータ | 型 | 説明 |
|-----------|---|------|
| `host` | string | 接続先ホスト名またはIPアドレス |
| `user` | string | SSHユーザー名 |
| `cmd` | string | 実行するコマンド |

### 認証パラメータ（いずれか必須）

`password`と`key_file`のどちらか、または両方を指定します。両方を指定した場合、サーバーはどちらでも受け付けられます。暗号化された鍵の場合は、あわせてパスフレーズを指定します。

| パラメータ | 型 | 説明 |
|-----------|---|------|
| `password` | string | パスワード認証 |
| `key_file` | string | 秘密鍵ファイルのパス（`~`展開対応） |
| `key_passphrase` | string | 秘密鍵のパスフレーズ（暗号化された鍵の場合） |

### オプションパラメータ

残りは接続の調整に使い、省略した場合は以下の既定値になります。

| パラメータ | 型 | デフォルト | 説明 |
|-----------|---|-----------|------|
| `port` | int | 22 | SSHポート番号 |
| `timeout` | string | "30s" | 接続とコマンド実行の制限時間。`"30s"`や`"5m"`のようなGoのduration形式で指定し、数値だけの指定は受け付けない。超えた場合の扱いは後述 |
| `workdir` | string | - | リモートでの作業ディレクトリ |
| `strict_host_check` | bool | true | Host Keyの厳密検証 |
| `known_hosts` | string | `~/.ssh/known_hosts`と`/etc/ssh/ssh_known_hosts` | Known Hostsファイルのパス。既定では2つのうち存在するファイルを読み、どちらもなければホストの検証に失敗する |

`timeout`を過ぎても終わらないコマンドには`SIGTERM`を送り、ステップにはテストできる結果が残ります。`res.timed_out`が`true`になり、`res.stdout`と`res.stderr`にはそれまでに書いた内容が、`res.code`にはシグナルを受けて終了したときの終了コード（多くは`143`）が入ります。その後5秒経っても終わらないコマンドは待つのをやめ、`res.code`は`-1`になります。

### 環境変数

リモートのコマンドに渡す環境変数は`env`で設定します。

```yaml
with:
  env:
    NODE_ENV: production
    APP_VERSION: v1.2.3
```

数値や真偽値は文字列として渡します。`env__NODE_ENV: production`という平坦な書き方も受け付けます。同じ名前を両方で指定した場合は`env`の値が優先されます。

環境変数はSSHのセッションを通して送るため、サーバーの`sshd_config`が`AcceptEnv`で許可した名前しか受け付けられません。拒否された名前は無視され、コマンドはその変数なしで実行されます。このときProbeは、値は出さずに名前だけを示す警告をログに出します。コマンドが必要とする名前は、サーバー側で`AcceptEnv`に追加してください。

拒否を避けるために値を`cmd`へ直接書き込むのは避けてください。`cmd`に埋め込んだ値はリモートのシェルがそのまま解釈するため、レスポンスなど信頼できない値はコマンドインジェクションを招きます。また、シークレットはリモートホストのプロセス一覧から見えてしまいます。

## 戻り値

SSH Actionは以下の値を返します：

| フィールド | 型 | 説明 |
|-----------|---|------|
| `res.code` | int | 終了コード（0 = 成功） |
| `res.stdout` | string | 標準出力 |
| `res.json` | any | `res.stdout`全体が1つのJSONオブジェクトまたは配列のときにデコードした値。それ以外では設定されない |
| `res.stderr` | string | 標準エラー出力 |
| `res.timed_out` | bool | コマンドが`timeout`で停止されたときに`true` |
| `status` | int | 実行ステータス（0 = 成功、1 = 失敗） |
| `rt.duration` | string | ステップにかかった時間。`"1.2s"`など |
| `rt.sec` | float | 同じ時間を秒で表した値 |

## 使用例

以下では認証方法ごとの書き方を示し、続けて1つのコマンドで済まない場合、つまり環境変数の利用、複数行のスクリプト、失敗した内容を確認したい場合を扱います。

### パスワード認証

最も単純な方法ですが、パスワードはファイルではなく環境変数から渡します。

```yaml
- name: パスワード認証でログイン
  uses: ssh
  with:
    host: "server.example.com"
    port: 22
    user: "admin"
    password: "secure_password"
    cmd: "systemctl status nginx"
    timeout: "60s"
  test: res.code == 0
```

### 公開鍵認証

`key_file`で秘密鍵を指定します。`~`は展開されます。

```yaml
- name: SSH鍵でログイン
  uses: ssh
  with:
    host: "prod.example.com"
    user: "deploy"
    key_file: "~/.ssh/deploy_key"
    cmd: "git pull origin main && systemctl restart app"
    workdir: "/opt/myapp"
    timeout: "300s"
  test: res.code == 0
```

### 暗号化された秘密鍵

パスフレーズで保護された鍵では、ファイルに加えて`key_passphrase`が必要です。

```yaml
- name: パスフレーズ付き鍵でログイン
  uses: ssh
  with:
    host: "secure.example.com"
    user: "admin"
    key_file: "~/.ssh/id_rsa"
    key_passphrase: "key_passphrase"
    cmd: "docker ps"
  test: res.code == 0
```

### 環境変数を使用

どのパラメータにもテンプレートを書けるため、接続先や認証情報をファイルの外から渡せます。

```yaml
- name: 環境変数付きでデプロイ
  uses: ssh
  with:
    host: "app.example.com"
    user: "deploy"
    key_file: "~/.ssh/deploy_key"
    cmd: |
      echo "Deploying version: $APP_VERSION"
      echo "Environment: $DEPLOY_ENV"
      ./deploy.sh
    env:
      APP_VERSION: "v2.1.0"
      DEPLOY_ENV: "production"
    workdir: "/opt/myapp"
    timeout: "600s"
  test: res.code == 0 && res.stdout contains "Deploy completed"
```

### 複数行コマンド

ブロックスカラーで書けば1つのセッションで複数のコマンドを実行でき、出力はまとめて`res.stdout`に入ります。

```yaml
- name: システム情報収集
  uses: ssh
  with:
    host: "monitor.example.com"
    user: "admin"
    password: "admin_pass"
    cmd: |
      echo "=== System Information ==="
      uname -a
      echo "=== Disk Usage ==="
      df -h
      echo "=== Memory Usage ==="
      free -h
      echo "=== Process List ==="
      ps aux | head -10
    timeout: "120s"
  test: res.code == 0
  echo: "{{res.stdout}}"
```

### エラーハンドリング

接続に成功してもリモートのコマンドは失敗しえます。そのためテストでは終了コードとエラー出力の両方を確認します。

```yaml
- name: サービス再起動（エラー処理付き）
  uses: ssh
  with:
    host: "app.example.com"
    user: "deploy"
    key_file: "~/.ssh/deploy_key"
    cmd: |
      if systemctl is-active --quiet myapp; then
        echo "Stopping service..."
        systemctl stop myapp
      fi
      echo "Starting service..."
      systemctl start myapp
      systemctl is-active myapp
    timeout: "180s"
  test: |
    res.code == 0 && 
    res.stdout contains "active" &&
    !(res.stderr contains "failed")
```

## セキュリティ設定

接続先のサーバーを信頼してよいかはHost Key検証で決まります。この挙動は`strict_host_check`と`known_hosts`で制御します。

### Host Key検証

`strict_host_check`が有効な場合、接続先の鍵が`known_hosts`に登録されている必要があります。

```yaml
- name: 厳密なHost Key検証
  uses: ssh
  with:
    host: "secure.example.com"
    user: "admin"
    key_file: "~/.ssh/id_rsa"
    cmd: "whoami"
    strict_host_check: true
    known_hosts: "~/.ssh/known_hosts"
  test: res.code == 0
```

### Host Key検証を無効化（開発環境のみ推奨）

検証を無効にするとどの鍵でも受け入れます。自分が管理するホストに対してのみ使えます。

```yaml
- name: 開発環境での接続
  uses: ssh
  with:
    host: "dev.example.com"
    user: "developer"
    password: "dev_password"
    cmd: "npm test"
    strict_host_check: false  # 開発環境でのみ使用
  test: res.code == 0
```

## ベストプラクティス

手元での試行ではなく運用でSSHステップを使うと、どの認証情報を使うか、どれだけ待つか、失敗をどう扱うか、ログに何が残るかが問題になります。

### 1. 認証方法の選択

**公開鍵認証を推奨**：
```yaml
# 推奨
key_file: "~/.ssh/deploy_key"

# 可能であれば避ける
password: "plain_password"
```

### 2. タイムアウトの設定

適切なタイムアウト値を設定：
```yaml
# 短時間のコマンド
timeout: "30s"

# デプロイメントなど長時間の処理
timeout: "600s"
```

### 3. エラーハンドリング

終了コードが0でも問題が報告されていることがあります。そのため`res.stderr`もあわせて確認します。

```yaml
test: |
  res.code == 0 && 
  !(res.stderr contains "error") &&
  !(res.stderr contains "failed")
```

### 4. ログの安全性

パスワードや秘密鍵の情報は自動的にログから除外されます。

### 5. 環境変数の活用

機密情報は環境変数を使用：
```yaml
vars:
  ssh_password: "{{vars.SSH_PASSWORD}}"
  deploy_key: "{{vars.DEPLOY_KEY_PATH}}"

steps:
- uses: ssh
  with:
    password: "{{vars.ssh_password}}"
    key_file: "{{vars.deploy_key}}"
```

## 制限事項

SSHアクションが意図的に対応していない点が2つあります。

### SSH Configファイル非対応

移植性と再現性を重視するため、SSH Configファイル（`~/.ssh/config`）はサポートしていません。すべての接続パラメータはワークフロー内で明示的に定義する必要があります。

### 対話的コマンド

対話的な入力が必要なコマンドはサポートしていません。すべてのコマンドは非対話的に実行される必要があります。

## トラブルシューティング

SSHの失敗はコマンドそのものより接続の確立で起きることが多くあります。症状ごとに最初に確認する箇所を示します。

### 接続エラー

**Host Key検証エラー**:
```yaml
# known_hosts を確認
known_hosts: "~/.ssh/known_hosts"

# または開発環境では無効化
strict_host_check: false
```

**認証エラー**:
```yaml
# 鍵ファイルのパスとパーミッションを確認
key_file: "/home/user/.ssh/id_rsa"  # 絶対パス
# または
key_file: "~/.ssh/id_rsa"          # チルダ展開

# パスフレーズが必要な場合
key_passphrase: "your_passphrase"
```

**タイムアウトエラー**:
```yaml
# より長いタイムアウトを設定
timeout: "300s"
```

### 連続実行時の問題

同じホストへの連続接続で問題が発生する場合は、適切な間隔を空ける：

```yaml
- name: 最初の接続
  uses: ssh
  with:
    cmd: "command1"
  
- name: 少し待機
  uses: hello
  with:
    name: "wait"
  wait: 1s
  
- name: 次の接続
  uses: ssh
  with:
    cmd: "command2"
```

## 関連項目

- [Shell Action](/ja/reference/actions/shell) - ローカルでのコマンド実行
- [HTTP Action](/ja/reference/actions/http) - REST APIとの通信
- [環境変数の管理](/ja/reference/environment-variables)
