// 不依赖框架的小工具：el 建节点，属性里 on* 是事件，text 是文字内容。
export function el(tag, attrs = {}, ...children) {
  const node = document.createElement(tag);
  for (const [name, value] of Object.entries(attrs)) {
    if (value === undefined || value === null || value === false) continue;
    if (name.startsWith('on')) node.addEventListener(name.slice(2).toLowerCase(), value);
    else if (name === 'class') node.className = value;
    else if (name === 'text') node.textContent = value;
    else if (name in node && !name.startsWith('aria-')) node[name] = value;
    else node.setAttribute(name, value === true ? '' : value);
  }
  node.append(...children.flat().filter(x => x !== null && x !== undefined));
  return node;
}
export const text = (tag, content, className) => el(tag, { text: content, class: className });
export const button = (label, action, className = '', disabled = false) => el('button', { type: 'button', class: className, text: label, disabled, onclick: action });
export const link = (label, href) => el('a', { href, target: '_blank', rel: 'noopener noreferrer', text: label });
export function input(name, label, attrs = {}) {
  return el('label', { class: 'field' }, text('span', label), el('input', { name, ...attrs }));
}
export function select(name, label, values, current) {
  const field = el('select', { name }, values.map(([value, label]) => el('option', { value, text: label, selected: current === value })));
  return el('label', { class: 'field' }, text('span', label), field);
}
