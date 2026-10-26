<div align="center">

<img src="docs/brand/vortex-tile.svg" width="120" alt="GoWind 風行" />

# GoWind Admin｜風行

**すぐに使える企業級管理システムスキャフォールド（Go バックエンド + React 19）**

[![License](https://img.shields.io/badge/License-MIT-blue.svg)](./LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go)](https://go.dev/)
[![Frontend](https://img.shields.io/badge/Frontend-React_19_only-61DAFB?logo=react)](./frontend/admin/react)
[![React](https://img.shields.io/badge/React-19.x-61DAFB?logo=react)](https://react.dev/)
[![Docker](https://img.shields.io/badge/Docker-Ready-2496ED?logo=docker)](https://www.docker.com/)

[English](./README.en-US.md) | [中文](./README.md) | **日本語**

</div>

---

## プロジェクトのハイライト

- **フロントエンドは React 19 + Ant Design のみ**。バックエンドはそのまま。`vue-element` / `vue-vben` は [docs/adopt-one-frontend.md](./docs/adopt-one-frontend.md)（中国語）に従って削除済みで、維持するのは `frontend/admin/react` だけ
- **エンタープライズ級 RBAC**：マルチテナント、マルチロール、マルチ部署、メニュー / ボタン / データレベルの権限制御（ポリシーエンジン切り替え可：Casbin / OPA）
- **セキュリティと等保コンプライアンス**：等保 2.0 の技術要求に準拠——監査ログの 180 日保持・アーカイブ、パスワードポリシー 3 点セット、画像認証コードとログイン失敗レート制限、パスワードのアプリケーション層暗号化、動的 RBAC とテナント分離。詳しくは[セキュリティと等級保護コンプライアンス](#セキュリティと等級保護コンプライアンス等保-20) を参照
- **マイクロサービス + モノリスの自由切替**：go-kratos マイクロサービスフレームワークベースでありながら、モノリス構成での開発・デプロイもサポートし、チーム規模に柔軟に対応
- **フルスタックコード生成**：Protobuf → Go API / TypeScript クライアント、Ent Schema → ORM。`backend/` で `make api`、`make ent`、`make ts`。
- **本番即戦力**：JWT 認証、SSE プッシュ、非同期タスクスケジューリング、Swagger ドキュメント、Docker ワンクリックデプロイ
### この作業コピーは React のみ

**上流は 3 つの並列フロントエンドを配る。このツリーはすでに React を選んでいる。**

`vue-element` と `vue-vben` は削除済み。バックエンド、API 契約、React ページは残っている。上流の新機能は React が先に入る。

文書中の残り 2 系のポート、デモ URL、`make ts` の生成先はここでは使わない。手順は [docs/adopt-one-frontend.md](./docs/adopt-one-frontend.md)（中国語）。

---

## ここから始める

- **このコードベースを体系的に学びたい場合**：[ドキュメントインデックス](./docs/README.md)（中国語）から始めてください。ドキュメントは**チュートリアル層**（空の環境からビジネスモジュールの独立開発・安全なデプロイまで案内する[全 8 章の段階的チュートリアル](./docs/tutorial/README.md)）と**リファレンス層**（各サブシステムの権威ある説明）に分かれており、インデックスには役割別（フルスタック採用者 / バックエンド / フロントエンド / 運用・セキュリティ）の推奨リーディングパスが記載されています。
- **まず動作を見たい場合**：下に[デモ](#デモ)と[クイックスタート](#クイックスタート)が続きます。

---

## リポジトリ

本プロジェクトは以下の 3 つのプラットフォームで同時公開しています。内容は同一ですので、クローン / スターはお好みのものをどうぞ：

| プラットフォーム | URL |
|------------------|-----|
| リポジトリ | この作業コピー |
| Gitee | <https://gitee.com/tx7do/go-wind-admin> |
| AtomGit | <https://atomgit.com/tx7do/go-wind-admin> |

---

## デモ

下の 3 URL は上流のオンラインデモです。**このツリーのコードは React のみ**で、ローカルは最後の行に対応します：

| フロントエンド版 | デモ |
|------------------|------|
| Vue3 Vben | <https://vben.admin.gowind.cloud> |
| Vue3 Element Plus | <https://ele.admin.gowind.cloud> |
| React | <https://react.admin.gowind.cloud> |

- バックエンド Swagger：<https://api.demo.admin.gowind.cloud/docs/>
- デフォルトアカウント：`admin` / `Abcd@1234`

---

## クイックスタート

### 環境要件

| ツール | バージョン |
|--------|-----------|
| Go | 1.26+（`backend/go.mod` に従う） |
| Node.js | `^20.19.0 \|\| >=22.12.0`（Vite 8 の要件）。**21.x は不可**。20.18 以下も不可 |
| pnpm | `>= 9`。React 側は pnpm のバージョンを固定していない |
| Docker | 20.0+ |

### 環境スクリプト選択

- Linux / macOS 開発環境：`backend/scripts/env/install_unix_dev.sh`
- Linux / macOS 本番環境：`backend/scripts/env/install_unix_prod.sh`
- Windows 開発環境：`backend/scripts/env/install_windows_dev.ps1`

### Docker 2つのデプロイモード

- **full_deploy 完全モード**：ミドルウェア+バックエンドアプリを同時起動、ワンクリックデモ・本番デプロイに適用。
- **libs_only 依存モード（開発推奨）**：ミドルウェアのみ起動、アプリはローカル IDE で実行・デバッグ。

### バックエンド起動

> バックエンドのコマンドは `backend/` の Makefile で実行します（`make api`、`make ent`、`make -C app/admin/service run`）。詳細は [AGENTS.md](./AGENTS.md)。

**Linux / macOS：**

```shell
# 以下のコマンドはすべて backend/ ディレクトリで実行（scripts/ は backend/ 配下）
cd backend

# スクリプトに実行権限を付与
# scripts/ は三階層。glob では env/lib と deploy/sse が抜けるため find を使用
find ./scripts -name '*.sh' -exec chmod +x {} +

# 開発環境（推奨）
./scripts/env/install_unix_dev.sh
./scripts/docker/libs_only.sh
make -C app/admin/service run

# 本番環境
./scripts/env/install_unix_prod.sh
./scripts/docker/full_deploy.sh

# PM2 プロセス管理（本番上級）
./scripts/deploy/pm2_service.sh
```

**Windows（PowerShell 管理者）：**

```powershell
# 以下のコマンドはすべて backend/ ディレクトリで実行（scripts/ は backend/ 配下）
cd backend

# スクリプト実行ポリシーの許可（初回のみ1回実行）
Set-ExecutionPolicy RemoteSigned -Scope CurrentUser

# 環境初期化
.\scripts\env\install_windows_dev.ps1

# ローカル開発
.\scripts\docker\libs_only.ps1
make -C app/admin/service run

# ワンクリック完全デプロイ
.\scripts\docker\full_deploy.ps1
```

### フロントエンド起動

このツリーは React のみ。ディレクトリは `frontend/admin/react`。リポジトリ直下と `frontend/admin/` に `package.json` は無く、そこで `pnpm install` すると ENOENT になる。

| フロントエンド版 | ディレクトリ | 起動コマンド | ポート |
|------------------|--------------|--------------|--------|
| React | `frontend/admin/react` | `pnpm dev` | 15888 |

```shell
# React ディレクトリへ入ってから install → 起動。
# リポジトリ直下や frontend/admin/ では pnpm install しない。
# API は http://localhost:7788 、アカウント admin / Abcd@1234
cd frontend/admin/react
pnpm install
pnpm dev                    # ポート 15888
```

> Node.js は `^20.19.0` または `>=22.12.0`。21.x は不可。パッケージ管理は pnpm。
>
> 開発ポートは `frontend/admin/react/.env.development` の `VITE_SERVER_PORT`（15888）。使用中なら Vite が +1 する。バックエンド CORS は `15888` と `15889` を許可済み。
>
> `vue-element` と `vue-vben` は削除済み。`backend/` で `make ts` を実行すると React クライアントだけを生成する。

---

## 技術スタック

<table>
<tr><th>レイヤー</th><th>技術</th></tr>
<tr><td><strong>バックエンドフレームワーク</strong></td><td><code>Golang</code> · <code>go-kratos v2</code> · <code>Protobuf / Buf</code></td></tr>
<tr><td><strong>ORM</strong></td><td><code>Ent</code> · <code>MySQL</code> · <code>PostgreSQL</code></td></tr>
<tr><td><strong>ミドルウェア</strong></td><td><code>Redis</code>（compose は <code>bitnami/redis:latest</code> を取得、バージョン固定なし。コード側は Set/Expire/Publish などの長期コマンドのみで、Redis 8 専用コマンドは不使用） · <code>MinIO</code>（S3 互換オブジェクトストレージ）</td></tr>
<tr><td><strong>認証・認可</strong></td><td><code>JWT</code>（認証） · <code>Casbin</code> / <code>OPA</code>（認可エンジン、2 択 1、デフォルトは全許可の noop）</td></tr>
<tr><td><strong>リアルタイム通信</strong></td><td><code>SSE</code>（サーバープッシュ） · <code>Asynq</code>（非同期タスク）</td></tr>
<tr><td><strong>フロントエンド</strong></td><td><strong>React のみ</strong>。続く 2 行は上流の選択肢で、このツリーからは削除済み</td></tr>
<tr><td><strong>Vue Vben 版</strong></td><td>このツリーから削除済み（上流のデモサイトは残っている）</td></tr>
<tr><td><strong>Vue Element 版</strong></td><td>このツリーから削除済み（上流のデモサイトは残っている）</td></tr>
<tr><td><strong>React 版</strong></td><td><code>React 19</code> · <code>TypeScript</code> · <code>Vite</code> · <code>Zustand</code> · <code>Ant Design V6</code>（UMI 不使用） · <code>Tailwind CSS</code></td></tr>
<tr><td><strong>デプロイ・運用</strong></td><td><code>Docker</code> · <code>Docker Compose</code> · <code>PM2</code> · <code>Swagger UI</code></td></tr>
</table>

---

## セキュリティと等級保護コンプライアンス（等保 2.0）

本プロジェクトのセキュリティ能力は、中国《ネットワークセキュリティ等級保護 2.0》（等保 2.0、レベル 2/3）の技術要求を参照して設計されており、企業の高プライバシー・プライベートデプロイシナリオにそのまま利用できます：

| 技術要求 | 実装内容 |
|---------|---------|
| **セキュリティ監査** | ログインログと操作ログ。クライアント IP と所属地、フロントエンド発行の `X-Request-ID` リクエスト ID を記録する。asynq による毎日定時アーカイブ：DB 内保持は 180 日（`AUDIT_RETENTION_DAYS` で調整可能）、期限超過データは JSONL アーカイブファイルへエクスポートして痕跡を保持 |
| **本人認証** | パスワード複雑度（8 文字以上、小文字 / 大文字 / 数字 / 記号の 4 種類から 3 種類以上）、履歴パスワード再利用チェック（デフォルト直近 3 件）、パスワード有効期間（デフォルト 90 日）— しきい値は「パラメータ管理」のプラットフォームパラメータで調整（内蔵パラメータは起動時にシードされ、環境変数による設定は廃止）。画像認証コード、Redis ログイン失敗レート制限（IP + ユーザー名の 2 次元） |
| **アクセス制御** | 動的 RBAC 権限エンジン（ポリシーエンジン切替可能：Casbin / OPA）。ロール—権限—インターフェースのマッピングは DB に保存され、権限変更は即時ホットリロードで反映。メニュー / ボタンレベルの権限制御に加え、ロール単位の行レベルデータスコープ（V1 試行：役職テーブル）とフィールドレベル権限（V1 試行：ユーザーテーブル、ブラックリストフィールドはレスポンスから刈り取り） |
| **マルチテナント分離** | ent Privacy ポリシーによるコンパイルレベルのデータ分離：読み取りクエリには自動的にテナントフィルターが注入され、Create はテナント偽装を防止、Update / Delete にはテナント述語が注入される（テナント横断の変更は 0 行ヒット）。テナントリクエストは `(path, method)` により Api テーブルでフェイルクローズ検証（権限ポイント欠落は即拒否）。プランのモジュールホワイトリストと期限切れ読み取り専用ポリシー |
| **データ機密性** | ログインパスワードはアプリケーション層で AES 暗号化送信、bcrypt ハッシュで保存。機密タスク設定は AES-256-GCM で保存時暗号化（Ent Hook による透過的加復号）。JWT RS256 非対称署名、refresh token は HttpOnly Cookie。トランスポート層 TLS はデプロイ層で有効化（バックエンド `server.rest.tls` 設定、または nginx / ロードバランサー終端） |
| **データバックアップ・リカバリ** | プログラム内にデータベースバックアップはない。データベース付属ツール（例: `pg_dump`）か運用基盤で全量バックアップし、リストアを一度リハーサルする |
| **フロントエンドセキュリティ** | 3 つのフロントエンドはそれぞれ `scripts/deploy/nginx.conf` を同梱し、本番では X-Frame-Options / HSTS / Content-Security-Policy レスポンスヘッダーを下流する。react と vue-element はビルド時に `index.html` へ CSP `<meta>` も注入する（インラインスクリプトは sha256 ホワイトリスト）。web server を差し替えても一層目の防御が残る |

> **注記**：等保評価には技術要求のほか、管理制度、物理環境、人員組織などソフトウェア以外の領域が含まれます。本プロジェクトがカバーするのは技術措置の部分であり、プライベートデプロイにおける等保評価準備を直接支援しますが、完全な等保評価プロセスの代替ではありません。

---

## 機能一覧

> 各リストページ（業務データおよび監査ログ）は、現在のフィルター条件でページング集約による「エクスポート」をサポートする。形式は CSV / XLSX から選択可能（上限 1 万行）。

### 組織と権限

| 機能 | 説明 |
|------|------|
| ユーザー管理 | ユーザーの管理とクエリを行い、高度なクエリや部署に連動したユーザー検索をサポート。ユーザーの無効化 / 有効化、上司の設定 / 解除、パスワードリセット、複数ロール・複数部署・上位上司の設定、指定ユーザーでのワンクリックログインなどの機能を提供。 |
| テナント管理 | テナントの管理を行い、新規テナント追加後に自動的にテナント部署、デフォルトロール、管理者を初期化。プランの設定、無効化 / 有効化、テナント管理者でのワンクリックログイン機能をサポート。サイドバーには出さない。ページは `/tenant/members` に残る。 |
| プラン・クォータ管理 | テナントのサブスクリプションプランおよびリソースクォータ（モジュールホワイトリスト、使用量上限など）を管理。プランおよびクォータ項目の CRUD をサポート。サイドバーには出さない。ページは `/tenant/plans` に残る。 |
| ロール管理 | ロールとロールグループの管理を行い、ロールに連動したユーザー検索、メニュー付与・データ権限スコープ（5 段階 / カスタム組織ユニット集合）・フィールドレベル権限（ブラックリストフィールド集合）の設定、従業員の一括追加・削除をサポート。 |
| 権限管理 | 権限グループ、メニュー、権限ポイントの管理を行い、ツリーリストでの表示をサポート。 |
| 組織管理 | 組織の管理を行い、ツリーリストでの表示をサポート。 |
| 役職管理 | ユーザーの役職管理を行い、役職はユーザーのタグとして使用可能。Excel インポートをサポート（クライアント側でのテンプレートダウンロード、既存の作成 API による 1 行ずつの処理と行単位のエラー報告）。**「所属組織」列を解決するのは vue-element のインポータのみ**（組織名の完全一致照合で組織ユニットを補完、不一致はその行のエラーとして報告）。react と vue-vben のインポート項目リストは `orgUnitId` を意図的に除外しており、コード中のコメントは「外部キーの名称解決は後続演进」としている。 |
| メニュー管理 | システムメニュー、操作権限、ボタン権限識別子などの設定を行い、ディレクトリ、メニュー、ボタンを含む。メニュー同期（3 フロントエンド対応）はトランザクション化された全件再構築とインクリメンタルマージの 2 モードをサポートし、マージはフルパス照合で既存メニューをその場更新して既存のメニュー ID とロール付与を保持する。 |

### システム機能

| 機能 | 説明 |
|------|------|
| インターフェース管理 | インターフェースの管理を行い、インターフェース同期機能をサポート。主に新規権限ポイント追加時のインターフェース選択に使用し、ツリーリスト表示、操作ログのリクエストパラメーターとレスポンス結果の設定をサポート。 |
| ディクショナリ管理 | データディクショナリの大分類と小分類の管理を行い、ディクショナリ大分類に連動した小分類検索、サーバー側の多列ソート、データのインポート・エクスポートをサポート。 |
| タスクスケジューリング | タスクとタスク実行ログの管理・参照を行い、タスクの新規追加、修正、削除、起動、一時停止、即時実行をサポート。 |
| ファイル管理 | ファイルアップロードの管理を行い、ファイルクエリ、OSS またはローカルへのアップロード、ダウンロード、ファイルアドレスのコピー、ファイル削除、画像の拡大表示をサポート。 |
| アカウントログイン | ユーザー名 / メールアドレス / 電話番号をアカウント識別子としてログイン可能。画像認証コードとログイン失敗レート制限付き。 |
| 認証コード送信 | 個人センターのメール・電話番号変更は `app/admin/service/configs/mail.yaml` の SMTP と SMS を読む。host または provider が空ならその経路は未設定。送信はタスクキューに入らない。 |
| オンラインユーザー | 現在オンラインのセッション一覧（ユーザー、テナント、クライアント種別、ログイン IP、User-Agent、デバイス ID、ログイン時刻）。キーワード絞り込み・ページング（ログイン時刻の降順）・セッション単位の強制ログアウトに対応、30 秒自動更新。 |
| パラメータ管理 | プラットフォーム全体のシステムパラメータをキー / 値で管理（業務ディクショナリとは区別）。内蔵パラメータは起動時にシードされ、削除は不可。読み取りはサービス側のキャッシュ付き accessor を経由し、マルチインスタンス構成では変更が Redis パブリッシュ / サブスクライブでブロードキャストされて各インスタンスのキャッシュが失効される。 |
| 言語管理 | システムがサポートする多言語を管理し、言語名、言語コード、ネイティブ名、有効化およびデフォルト状態を設定。 |

### メッセージとログ

| 機能 | 説明 |
|------|------|
| メッセージ分類 | メッセージ管理で選択するメッセージ分類の管理。分類は**平坦な 1 層のみ**（`sys_internal_message_categories` に parent_id 列は無く、削除も当該行のみでツリーカスケードは行わない）。 |
| メッセージ管理 | メッセージの管理を行い、送信範囲（全員 / 指定ユーザー）による送信とメッセージの取り消しをサポート。全員ブロードキャストは非同期タスクキューで配信（再開可能・冪等）。ユーザーの既読状況と既読時間の参照が可能。 |
| 内部メッセージ | 内部メッセージの管理を行い、メッセージの詳細参照、削除、既読マーク、一括既読をサポート。 |
| ログインログ | ログインログリストのクエリを行い、ユーザーのログイン成功・失敗ログを記録し、IP アドレスの所属地記録をサポート。 |
| 操作ログ | 操作ログリストのクエリを行い、ユーザーの操作正常・異常ログを記録。IP アドレスの所属地記録とリソースオブジェクトの特定、操作ログの詳細参照をサポート。 |

### 個人センター

| 機能 | 説明 |
|------|------|
| マイページ | 個人情報の表示・修正、最終ログイン情報の参照、パスワードの変更、メールアドレスのバインド / 再バインド（認証コード検証）などの機能を提供。 |
| マイアクティブセッション | 自分の現在アクティブなセッション一覧（ログイン IP、デバイス、ログイン時刻など）。現在のセッションは表示のみ（ログアウト不可）、それ以外は個別にログアウト可能。 |

---

## プロジェクト構成

```
go-wind-admin/
├── backend/                        # バックエンドプロジェクト
│   ├── api/                        # Protobuf API 定義と生成コード
│   │   ├── protos/                 # .proto ソースファイル（ドメイン別）
│   │   └── gen/go/                 # buf が生成する Go コード
│   ├── app/admin/service/          # Admin サービスアプリケーション
│   │   ├── cmd/server/             # エントリポイント（main.go、wiring_ent.go 依存性組み立て）
│   │   ├── configs/                # 設定ファイル（YAML）
│   │   └── internal/               # ビジネスコア（data/service/server）
│   ├── pkg/                        # 共通パッケージ
│   │   ├── oss/                    # オブジェクトストレージ（MinIO）
│   │   ├── eventbus/               # イベントバス
│   │   └── ...                     # その他ユーティリティパッケージ
│   ├── scripts/                    # デプロイスクリプト（env/docker/deploy）
│   └── sql/                        # デモデータ SQL（デフォルトデータはサービス起動時に自動シード）
├── frontend/admin/                 # フロントエンド（React のみ）
│   └── react/                      # React 19 + Ant Design V6
└── docs/                           # プロジェクトドキュメント
```

---

## スクリーンショット

<table>
<tr>
<td><img src="./docs/images/admin_login_page.png" alt="バックエンドユーザーログイン画面"/></td>
<td><img src="./docs/images/admin_dashboard.png" alt="バックエンド分析画面"/></td>
</tr>
<tr>
<td><img src="./docs/images/admin_user_list.png" alt="バックエンドユーザーリスト画面"/></td>
<td><img src="./docs/images/admin_user_create.png" alt="バックエンドユーザー作成画面"/></td>
</tr>
<tr>
<td><img src="./docs/images/admin_tenant_list.png" alt="バックエンドテナントリスト画面"/></td>
<td><img src="./docs/images/admin_tenant_create.png" alt="バックエンドテナント作成画面"/></td>
</tr>
<tr>
<td><img src="./docs/images/admin_org_unit_list.png" alt="組織単位リスト画面"/></td>
<td><img src="./docs/images/admin_org_unit_create.png" alt="組織単位作成画面"/></td>
</tr>
<tr>
<td><img src="./docs/images/admin_position_list.png" alt="バックエンド役職リスト画面"/></td>
<td><img src="./docs/images/admin_position_create.png" alt="バックエンド役職作成画面"/></td>
</tr>
<tr>
<td><img src="./docs/images/admin_role_list.png" alt="バックエンドロールリスト画面"/></td>
<td><img src="./docs/images/admin_role_create.png" alt="バックエンドロール作成画面"/></td>
</tr>
<tr>
<td><img src="./docs/images/admin_permission_list.png" alt="バックエンド権限リスト画面"/></td>
<td><img src="./docs/images/admin_permission_create.png" alt="バックエンド権限作成画面"/></td>
</tr>
<tr>
<td><img src="./docs/images/admin_menu_list.png" alt="バックエンドディレクトリリスト画面"/></td>
<td><img src="./docs/images/admin_menu_create.png" alt="バックエンドディレクトリ作成画面"/></td>
</tr>
<tr>
<td><img src="./docs/images/admin_task_list.png" alt="バックエンドスケジューリングタスクリスト画面"/></td>
<td><img src="./docs/images/admin_task_create.png" alt="バックエンドスケジューリングタスク作成画面"/></td>
</tr>
<tr>
<td><img src="./docs/images/admin_dict_list.png" alt="バックエンドデータディクショナリリスト画面"/></td>
<td><img src="./docs/images/admin_dict_entry_create.png" alt="バックエンドデータディクショナリエントリ作成画面"/></td>
</tr>
<tr>
<td><img src="./docs/images/admin_internal_message_list.png" alt="バックエンド内部メッセージリスト画面"/></td>
<td><img src="./docs/images/admin_internal_message_publish.png" alt="バックエンド内部メッセージ発行画面"/></td>
</tr>
<tr>
<td><img src="./docs/images/admin_login_audit_log_list.png" alt="バックエンドログインログ画面"/></td>
<td><img src="./docs/images/admin_api_audit_log_list.png" alt="バックエンド操作ログ画面"/></td>
</tr>
<tr>
<td><img src="./docs/images/admin_api_list.png" alt="APIリスト画面"/></td>
<td><img src="./docs/images/api_swagger_ui.png" alt="バックエンド内蔵Swagger UI画面"/></td>
</tr>
</table>

## 関連ツール

コード生成は `backend/` で実行します：`make api`、`make openapi`、`make ent`、`make ts`。サービスの起動は `make -C app/admin/service run`。詳細は [AGENTS.md](./AGENTS.md)。

## コミュニティとコントリビューション

GoWind Admin コミュニティへの参加を歓迎します。以下のドキュメントで、コードの貢献方法、Issue の報告、セキュリティ脆弱性の開示について説明しています：

- [コントリビューションガイド](./CONTRIBUTING.md) — 開発環境、コード生成規約、コミット規約と PR フロー
- [行動規範](./.github/CODE_OF_CONDUCT.md) — コミュニティでの交流に関する方針
- [セキュリティポリシー](./SECURITY.md) — 脆弱性報告フローと適用範囲
- [更新履歴](./CHANGELOG.md) — バージョン変更記録
- Issue テンプレート：[バグ報告](./.github/ISSUE_TEMPLATE/bug_report.md) · [機能リクエスト](./.github/ISSUE_TEMPLATE/feature_request.md)
- [PR テンプレート](./.github/PULL_REQUEST_TEMPLATE.md)

## お問い合わせ

- WeChat 個人アカウント：`yang_lin_bo`（備考：`go-wind-admin`）
- 掘金コラム：[go-wind-admin](https://juejin.cn/column/7541283508041826367)

## 謝辞

[![JetBrains](https://resources.jetbrains.com/storage/products/company/brand/logos/jb_beam.svg)](https://jb.gg/OpenSource)

JetBrains により無料の GoLand & WebStorm オープンソースライセンスを提供いただいています。心より感謝申し上げます。
