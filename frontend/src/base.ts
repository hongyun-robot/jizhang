/**
 * 部署根路径（子路径部署适配，如站点挂在 /jizhang 下与默认官网共存）。
 *
 * - 根路径部署/开发期：BASE_URL = "/"      → ROOT = ""
 * - 子路径部署：        BASE_URL = "/jizhang/" → ROOT = "/jizhang"
 *
 * BASE_URL 由 vite.config.ts 的 `base` 决定：开发不设为 "/"（e2e 不受影响），
 * 生产构建注入 VITE_BASE=/jizhang/。
 */
const raw = import.meta.env.BASE_URL || '/';

/** 前缀串："" 或 "/jizhang"（无尾斜杠），用于拼接任意路径。 */
export const ROOT = raw === '/' ? '' : raw.replace(/\/+$/, '');

/** react-router basename："/" 或 "/jizhang"。 */
export const BASENAME = ROOT || '/';

/** API 前缀："/api/v1" 或 "/jizhang/api/v1"。 */
export const API_BASE = `${ROOT}/api/v1`;

/** 登录页绝对路径（window.location 跳转用，router 的 navigate 不需要）。 */
export const LOGIN_PATH = `${ROOT}/login`;
