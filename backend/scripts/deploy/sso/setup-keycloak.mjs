#!/usr/bin/env node
/**
 * Keycloak SSO 一键初始化脚本（演练/演示用）。
 *
 * 前置：docker compose -f docker-compose.keycloak.yml up -d（等 ~30s 就绪）。
 * 动作：建 realm gwa、机密客户端 gwa-admin（secret gwa-secret）、
 *       测试用户 sso-user / Keycloak@123（email sso@example.com）。
 *
 * 之后按 docs/authentication.md「OIDC SSO」一节配置后端环境变量即可
 * 演示企业账号登录全流。
 */
const KC = process.env.KC_BASE_URL || 'http://localhost:8081';

const tokenRes = await fetch(`${KC}/realms/master/protocol/openid-connect/token`, {
  method: 'POST',
  headers: { 'content-type': 'application/x-www-form-urlencoded' },
  body: new URLSearchParams({
    grant_type: 'password', client_id: 'admin-cli',
    username: process.env.KC_ADMIN || 'admin',
    password: process.env.KC_ADMIN_PASSWORD || 'admin',
  }),
});
const { access_token } = await tokenRes.json();
if (!access_token) throw new Error('admin token failed — Keycloak 就绪了吗？');
const auth = { authorization: 'Bearer ' + access_token, 'content-type': 'application/json' };

const realmRes = await fetch(`${KC}/admin/realms/gwa`, { headers: auth });
if (realmRes.status === 404) {
  const r = await fetch(`${KC}/admin/realms`, { method: 'POST', headers: auth, body: JSON.stringify({ realm: 'gwa', enabled: true }) });
  if (!r.ok) throw new Error('create realm failed: ' + r.status);
  console.log('[kc-setup] realm gwa created');
} else {
  console.log('[kc-setup] realm gwa exists');
}

const clients = await (await fetch(`${KC}/admin/realms/gwa/clients?clientId=gwa-admin`, { headers: auth })).json();
if (clients.length === 0) {
  const r = await fetch(`${KC}/admin/realms/gwa/clients`, { method: 'POST', headers: auth, body: JSON.stringify({
    clientId: 'gwa-admin', secret: 'gwa-secret', enabled: true,
    protocol: 'openid-connect', publicClient: false,
    standardFlowEnabled: true, directAccessGrantsEnabled: true,
    redirectUris: [
      'http://localhost:15888/auth/sso/callback',
      'http://localhost:15777/auth/sso/callback',
      'http://localhost:15666/auth/sso/callback',
    ],
    webOrigins: ['+'],
  }) });
  if (!r.ok) throw new Error('create client failed: ' + r.status);
  console.log('[kc-setup] client gwa-admin created (secret gwa-secret)');
} else {
  console.log('[kc-setup] client gwa-admin exists');
}

const users = await (await fetch(`${KC}/admin/realms/gwa/users?email=sso@example.com`, { headers: auth })).json();
if (users.length === 0) {
  const r = await fetch(`${KC}/admin/realms/gwa/users`, { method: 'POST', headers: auth, body: JSON.stringify({
    username: 'sso-user', email: 'sso@example.com', enabled: true, emailVerified: true,
    firstName: 'SSO', lastName: 'Tester',
  }) });
  if (!r.ok) throw new Error('create user failed: ' + r.status);
  const created = await (await fetch(`${KC}/admin/realms/gwa/users?email=sso@example.com`, { headers: auth })).json();
  const pr = await fetch(`${KC}/admin/realms/gwa/users/${created[0].id}/reset-password`, {
    method: 'PUT', headers: auth, body: JSON.stringify({ type: 'password', value: 'Keycloak@123', temporary: false }),
  });
  if (!pr.ok) throw new Error('reset password failed');
  console.log('[kc-setup] user sso-user created (Keycloak@123)');
} else {
  console.log('[kc-setup] user sso-user exists');
}

console.log(`
[kc-setup] DONE. 后端环境变量：
  SSO_OIDC_ISSUER=${KC}/realms/gwa
  SSO_OIDC_CLIENT_ID=gwa-admin
  SSO_OIDC_CLIENT_SECRET=gwa-secret
  SSO_OIDC_REDIRECT_URL=http://localhost:15888/auth/sso/callback
  SSO_AUTO_CREATE=true
登录页点「使用企业账号登录」→ Keycloak 登录页 sso-user / Keycloak@123。
`);
