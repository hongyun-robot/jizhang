/** 品牌位：账簿页标记（左栏线 + 三行账目）+ 中文字标 + 拉丁小字，登录页与侧栏共用。 */

export function BrandMark({ size = 30 }: { size?: number }) {
  return (
    <span
      className="brand-mark"
      aria-hidden="true"
      style={{ width: size, height: size, borderRadius: Math.round(size * 0.3) }}
    >
      <svg
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        style={{ width: size * 0.57, height: size * 0.57 }}
      >
        <path d="M6 4.5v15" opacity=".5" />
        <path d="M10.5 8h8" />
        <path d="M10.5 12h8" />
        <path d="M10.5 16h5" />
      </svg>
    </span>
  );
}

export default function Brand({ size = 30 }: { size?: number }) {
  return (
    <>
      <BrandMark size={size} />
      <span className="brand-text">
        <span className="brand-name">记账</span>
        <span className="brand-sub">Family Ledger</span>
      </span>
    </>
  );
}
