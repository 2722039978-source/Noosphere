const explicitChinese = /^(?:(?:请|麻烦)(?:你|帮我)?\s*|帮我\s*)?(?:记住|记下|记一下|保存(?:为)?(?:一条)?(?:长期|项目)?记忆)(?:\s*[：:,，]\s*|\s+|(?=以后|今后|我|本|这个|当前|默认))/u;
const explicitEnglish = /^(?:please\s+)?(?:remember|save (?:this|that) as (?:a )?memory)\s*(?:[:;,]\s*|\s+)/i;
const stableChinese = /^(?:(?:在)?(?:这个|本|当前)(?:项目|工作区)(?:里|中)?[，,\s]*(?:今后|以后|一直|统一|默认|总是|请始终|长期|约定(?:是)?|优先)|我(?:一直|通常)?(?:偏好|更喜欢))/u;
const stableEnglish = /^(?:(?:in|for) this project,\s*(?:always|prefer|default to)|my (?:project )?preference is)\b/i;
const credential = /(?:api[_ -]?key|access[_ -]?token|password|密码|密钥|访问令牌)\s*[:：=]\s*\S+|\bsk-(?:proj-)?[a-z\d_-]{16,}\b|-----BEGIN [A-Z ]*PRIVATE KEY-----/i;

export function memoryCandidate(text: string) {
  const excerpt = text.trim();
  if (!excerpt || [...excerpt].length > 600 || /[\r\n\x00-\x08]|\x60{3}|[？?]$|吗[。！!]?$/u.test(excerpt) || credential.test(excerpt)) return null;
  const command = explicitChinese.exec(excerpt) ?? explicitEnglish.exec(excerpt);
  if (!command && !stableChinese.test(excerpt) && !stableEnglish.test(excerpt)) return null;
  const content = excerpt.slice(command?.[0].length || 0).trim();
  if (!content || /^(?:如果|假如|假设|if\s|suppose\s)/iu.test(content)) return null;
  const preference = /^(?:我|my |i prefer)/iu.test(content);
  return { content, excerpt, category: preference ? 'preference' as const : 'convention' as const };
}

const revisionChinese = /^(?:(?:请|麻烦)(?:你|帮我)?\s*|帮我\s*)?(?:把)?(?:(?:修改|改写|更新|优化|修订)(?:一下)?(?:这条|这项|这段|项目|长期|一条)*记忆|(?:这条|这项|这段)记忆(?:改(?=成|为)|修改|改写|更新|优化|修订)(?:一下)?)/u;
const revisionEnglish = /^(?:please\s+)?(?:rewrite|edit|revise|update)\s+(?:this|that|my|the)\s+(?:project\s+)?memory\b/i;

export function memoryRevisionInstruction(text: string): string | null {
  const value = text.trim();
  const match = revisionChinese.exec(value) ?? revisionEnglish.exec(value);
  if (!match) return null;
  const tail = value.slice(match[0].length);
  if (!tail || /^[。！!.]$/u.test(tail)) return '';
  if (/^\s*[：:，,]/u.test(tail)) return tail.replace(/^\s*[：:，,]+\s*/u, '').trim();
  if (/^\s*(?:改成|改为|成|为|to\b|so that\b)/iu.test(tail)) {
    return /记忆改$/u.test(match[0]) ? '改' + tail.trim() : tail.trim();
  }
  return null;
}

export function memoryChange(previous: string, revised: string) {
  const before = [...previous];
  const after = [...revised];
  const length = Math.min(before.length, after.length);
  let prefix = 0;
  while (prefix < length && before[prefix] === after[prefix]) prefix++;
  let suffix = 0;
  while (suffix < length - prefix && before[before.length - suffix - 1] === after[after.length - suffix - 1]) suffix++;
  return {
    prefix: before.slice(0, prefix).join(''),
    before: before.slice(prefix, before.length - suffix).join(''),
    after: after.slice(prefix, after.length - suffix).join(''),
    suffix: before.slice(before.length - suffix).join(''),
  };
}
