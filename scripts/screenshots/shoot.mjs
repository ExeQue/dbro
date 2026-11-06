// Drives `dbro serve` in Chromium and saves the README screenshots.
// Usage: node shoot.mjs <base url> <output dir>  (run through scripts/screenshots.sh)
import { existsSync } from 'node:fs';
import { chromium } from 'playwright-core';

const [base, out] = process.argv.slice(2);
const executablePath = process.env.CHROME ?? ['/usr/bin/chromium', '/usr/bin/google-chrome-stable', '/usr/bin/google-chrome', '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'].find(existsSync);
if (!executablePath) throw new Error('No Chromium or Chrome found; set CHROME=/path/to/chrome');

const browser = await chromium.launch({ executablePath });
const page = await browser.newPage({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 2, colorScheme: 'dark' });
page.on('pageerror', (e) => console.error('page error:', e.message));
await page.goto(base);
await page.locator('.node', { hasText: 'shop' }).first().waitFor();

const settle = () => page.waitForTimeout(400);
const shot = async (name) => {
  await settle();
  await page.screenshot({ path: `${out}/${name}.png` });
  console.log(`saved ${name}.png`);
};

// jump anywhere through the command palette
const go = async (text) => {
  await page.keyboard.press('Control+k');
  await page.locator('.dialog input:focus').waitFor(); // the dialog focuses its input right after it opens
  await page.keyboard.insertText(text);
  await settle();
  const picked = (await page.locator('.palette .list-item.on').textContent()) ?? '';
  if (!picked.startsWith(text)) throw new Error(`palette picked "${picked}" for "${text}"`);
  await page.keyboard.press('Enter');
  await settle();
};

// on failure, keep a picture of where it got stuck
process.on('uncaughtException', async (e) => {
  console.error(e);
  await page.screenshot({ path: `${out}/_failed.png` }).catch(() => {});
  process.exit(1);
});

await page.locator('.node', { hasText: 'shop' }).first().dblclick();
await settle();

// 1. data: the orders table, filtered and sorted
await go('orders');
const where = page.locator('.where-field .cm-content');
await where.click();
await page.keyboard.insertText("status IN ('paid', 'shipped') AND total > 250");
await page.keyboard.press('Control+Enter');
await settle();
const total = page.locator('table.grid th', { hasText: 'total' });
await total.click();
await settle();
await total.click();
await shot('data');

// 2. command palette
await page.keyboard.press('Control+k');
await page.locator('.dialog input:focus').waitFor();
await page.keyboard.insertText('ord');
await shot('palette');
await page.keyboard.press('Escape');

// 3. structure of orders
await page.getByRole('tab', { name: 'Structure' }).click();
await shot('structure');

// 4. a query with a named parameter
await page.keyboard.press('Control+t');
await page.locator('.query .cm-content').last().click();
await page.keyboard.insertText(
  'SELECT c.name, c.country, COUNT(o.id) AS orders, SUM(o.total) AS spent\n' +
  'FROM customers c\nJOIN orders o ON o.customer_id = c.id\n' +
  "WHERE c.country = :country AND o.status <> 'canceled'\n" +
  'GROUP BY c.id\nORDER BY spent DESC',
);
await page.keyboard.press('Escape');
await page.locator('[data-param="country"]').fill('DK');
await page.keyboard.press('Control+Enter');
await page.locator('.query table.grid').last().waitFor();
await shot('query');

// 5. every built-in theme, on the data view, at 1x to keep the files small
await page.getByRole('tab', { name: 'Data' }).click();
const themes = (await page.evaluate(() => window.go.main.App.ListThemes())).filter((t) => t.source === 'builtin');
for (const t of themes) {
  await go(`Theme: ${t.name}`);
  await settle();
  await page.screenshot({ path: `${out}/theme-${t.id}.png`, scale: 'css' });
  console.log(`saved theme-${t.id}.png`);
}

await browser.close();
