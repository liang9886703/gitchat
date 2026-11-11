import { chromium } from 'playwright';
import { readFile, writeFile } from 'node:fs/promises';
import { browserOptions } from '../tests/browser/fixture.mjs';

const browser = await chromium.launch(await browserOptions());
try {
  const page = await browser.newPage();
  const source = await readFile(new URL('../web/icons/icon.svg', import.meta.url), 'utf8');
  for (const [name, size, maskable] of [['icon-192.png',192,false],['icon-512.png',512,false],['maskable-512.png',512,true],['apple-touch-icon.png',180,true]]) {
    const svg = maskable ? source.replace('rx="112"','rx="0"') : source;
    const png = await page.evaluate(async ({svg,size}) => {
      const image = new Image();
      image.src = `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`;
      await image.decode();
      const canvas = document.createElement('canvas');
      canvas.width = canvas.height = size;
      canvas.getContext('2d').drawImage(image,0,0,size,size);
      return canvas.toDataURL('image/png').split(',')[1];
    }, {svg,size});
    await writeFile(new URL(`../web/icons/${name}`,import.meta.url), Buffer.from(png,'base64'));
    console.log(`${name}: ${size}×${size}`);
  }
} finally { await browser.close(); }
