import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  testDir: "./tests/e2e",
  timeout: 30_000,
  expect: { timeout: 5_000 },
  fullyParallel: true,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 2 : 0,
  reporter: [["list"], ["html", { open: "never" }]],
  use: {
    baseURL: "http://127.0.0.1:4173",
    ...(process.env.CI ? {} : { channel: "chrome" }),
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  webServer: {
    command: "npm run dev -- --port 4173",
    url: "http://127.0.0.1:4173",
    reuseExistingServer: !process.env.CI,
  },
  projects: [
    {
      name: "desktop-1366",
      grepInvert: /@mobile|@responsive/,
      use: {
        ...devices["Desktop Chrome"],
        viewport: { width: 1366, height: 768 },
      },
    },
    {
      name: "tablet-1024",
      grepInvert: /@mobile|@responsive/,
      use: {
        ...devices["Desktop Chrome"],
        viewport: { width: 1024, height: 768 },
      },
    },
    {
      name: "mobile-390",
      grep: /@mobile/,
      use: {
        ...devices["Desktop Chrome"],
        viewport: { width: 390, height: 844 },
        deviceScaleFactor: 1,
        hasTouch: true,
      },
    },
    {
      name: "mobile-430",
      grep: /@mobile/,
      use: {
        ...devices["Desktop Chrome"],
        viewport: { width: 430, height: 932 },
        deviceScaleFactor: 1,
        hasTouch: true,
      },
    },
    ...[
      ["responsive-desktop-1366", 1366, 768, false],
      ["responsive-desktop-1440", 1440, 900, false],
      ["responsive-desktop-1920", 1920, 1080, false],
      ["responsive-tablet-768", 768, 1024, true],
      ["responsive-tablet-1024", 1024, 768, true],
      ["responsive-mobile-390", 390, 844, true],
      ["responsive-mobile-430", 430, 932, true],
    ].map(([name, width, height, hasTouch]) => ({
      name: String(name),
      grep: /@responsive/,
      use: {
        ...devices["Desktop Chrome"],
        viewport: { width: Number(width), height: Number(height) },
        deviceScaleFactor: 1,
        hasTouch: Boolean(hasTouch),
      },
    })),
  ],
});
