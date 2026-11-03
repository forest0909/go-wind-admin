import type React from 'react';

/**
 * 登录页品牌插画：「林间」临时版——一片树林，前景两棵大树之间留出空隙（林 + 间），
 * 树形与侧栏 / favicon 的 public/logo.svg 同源。正式 logo 出来后替换方法见 docs/brand/README.md。
 * 颜色只用 global.css 的 --brand-* 令牌，亮暗两态都可读。
 * 动效尊重 prefers-reduced-motion；SVG 内嵌 <style> 是文档级作用域，类名带 lj- 前缀防碰撞。
 */

// 单棵树的轮廓：顶点在 (0,0)，高 42、半宽 13，三层树冠 + 树干
const TREE_PATH =
  'M0 0l8 13h-4l7 11h-5l7 11H2v7h-4v-7h-11l7-11h-5l7-11h-4z';

const ANIMATION_CSS = `
.lj-glow{transform-box:view-box;transform-origin:280px 268px;animation:lj-breathe 9s ease-in-out infinite alternate}
.lj-sway{transform-box:fill-box;transform-origin:50% 100%;animation:lj-sway 7s ease-in-out infinite alternate}
.lj-d2{animation-duration:9s;animation-delay:-3s}
.lj-d3{animation-duration:11s;animation-delay:-5s}
.lj-mote{animation:lj-float 8s ease-in-out infinite alternate}
.lj-m2{animation-duration:11s;animation-delay:-4s}
.lj-m3{animation-duration:9s;animation-delay:-2s}
@keyframes lj-sway{from{transform:rotate(-1.2deg)}to{transform:rotate(1.2deg)}}
@keyframes lj-float{from{transform:translateY(8px);opacity:.35}to{transform:translateY(-10px);opacity:.9}}
@keyframes lj-breathe{from{transform:scale(1);opacity:1}to{transform:scale(1.05);opacity:.72}}
@media (prefers-reduced-motion:reduce){
.lj-glow,.lj-sway,.lj-mote{animation:none}
}
`;

interface TreeProps {
  x: number;
  top: number;
  scale: number;
  fill: string;
  opacity?: number;
  className?: string;
}

const Tree: React.FC<TreeProps> = ({ x, top, scale, fill, opacity = 1, className }) => (
  <g transform={`translate(${x} ${top}) scale(${scale})`}>
    <path className={className} d={TREE_PATH} fill={fill} fillOpacity={opacity} />
  </g>
);

const SloganIcon: React.FC<React.SVGProps<SVGSVGElement>> = (props) => (
  <svg
    viewBox="0 0 560 560"
    xmlns="http://www.w3.org/2000/svg"
    role="img"
    aria-label="林间 Admin"
    width="100%"
    height="100%"
    {...props}
  >
    <style>{ANIMATION_CSS}</style>
    <defs>
      <linearGradient id="lj-slogan-grad" gradientUnits="userSpaceOnUse" x1="0" y1="110" x2="0" y2="410">
        <stop offset="0" stopColor="var(--brand-grad-light)" />
        <stop offset="0.5" stopColor="var(--brand-grad-mid)" />
        <stop offset="1" stopColor="var(--brand-grad-deep)" />
      </linearGradient>
      <radialGradient id="lj-slogan-glow" cx="0.5" cy="0.5" r="0.5">
        <stop offset="0" stopColor="var(--brand-grad-mid)" stopOpacity="0.13" />
        <stop offset="1" stopColor="var(--brand-grad-mid)" stopOpacity="0" />
      </radialGradient>
    </defs>
    <circle className="lj-glow" cx="280" cy="268" r="252" fill="url(#lj-slogan-glow)" />

    {/* 地面 */}
    <ellipse cx="280" cy="406" rx="230" ry="16" fill="var(--brand-grad-mid)" fillOpacity="0.14" />

    {/* 远景树 */}
    <Tree x={105} top={280} scale={3} fill="var(--brand-grad-light)" opacity={0.55} className="lj-sway lj-d2" />
    <Tree x={468} top={262} scale={3.4} fill="var(--brand-grad-light)" opacity={0.55} className="lj-sway lj-d3" />

    {/* 前景两棵树：林；中间的空隙：间 */}
    <Tree x={212} top={110} scale={7} fill="url(#lj-slogan-grad)" className="lj-sway" />
    <Tree x={378} top={188} scale={5.2} fill="url(#lj-slogan-grad)" opacity={0.85} className="lj-sway lj-d2" />

    {/* 林间光点 */}
    <circle className="lj-mote" cx="300" cy="200" r="5" fill="var(--brand-primary)" />
    <circle className="lj-mote lj-m2" cx="160" cy="230" r="4" fill="var(--brand-grad-light)" />
    <circle className="lj-mote lj-m3" cx="440" cy="190" r="4" fill="var(--brand-grad-light)" />
  </svg>
);

export default SloganIcon;
