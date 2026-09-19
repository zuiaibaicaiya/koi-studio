import { describe, expect, test } from '@rstest/core';
import { escapeHtml } from '../src/utils/html';

describe('escapeHtml', () => {
  test('转义全部危险字符', () => {
    expect(escapeHtml(`<img src=x onerror="alert('x')">`)).toBe(
      '&lt;img src=x onerror=&quot;alert(&#39;x&#39;)&quot;&gt;',
    );
  });

  test('& 需要最先转义且不产生双重转义', () => {
    expect(escapeHtml('&amp;')).toBe('&amp;amp;');
    expect(escapeHtml('a&b<c>d"e\'f')).toBe('a&amp;b&lt;c&gt;d&quot;e&#39;f');
  });

  test('普通文本原样返回', () => {
    expect(escapeHtml('你好 world 123')).toBe('你好 world 123');
    expect(escapeHtml('')).toBe('');
  });
});
