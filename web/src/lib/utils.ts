import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

export type MinorAmount = number | string | bigint;

const numberPartTypes = new Set(["integer", "group", "decimal", "fraction"]);

function exactMinorAmount(value: MinorAmount) {
  if (typeof value === "bigint") return value;
  if (typeof value === "number") {
    if (!Number.isSafeInteger(value)) {
      throw new RangeError(
        "Minor-unit money received as a number must be a safe integer.",
      );
    }
    return BigInt(value);
  }
  if (!/^-?\d+$/.test(value)) {
    throw new TypeError("Minor-unit money must be an integer string.");
  }
  return BigInt(value);
}

/**
 * Formats integer minor units without first converting them to a floating
 * point major-unit value. Strings and bigint preserve int64 precision across
 * the browser boundary; numbers are accepted only when they are safe integers.
 */
export function formatMoney(
  amountMinor?: MinorAmount | null,
  currency = "NGN",
  locale = "en-NG",
) {
  if (amountMinor == null) return "—";

  const amount = exactMinorAmount(amountMinor);
  const formatter = new Intl.NumberFormat(locale, {
    style: "currency",
    currency,
  });
  const fractionDigits = formatter.resolvedOptions().maximumFractionDigits ?? 2;
  const scale = 10n ** BigInt(fractionDigits);
  const negative = amount < 0n;
  const absolute = negative ? -amount : amount;
  const whole = absolute / scale;
  const fraction = (absolute % scale).toString().padStart(fractionDigits, "0");
  const positiveParts = formatter
    .formatToParts(whole)
    .map((part) =>
      part.type === "fraction" ? { ...part, value: fraction } : part,
    );

  if (!negative) return positiveParts.map((part) => part.value).join("");

  const negativeTemplate = formatter.formatToParts(-1);
  const firstNumber = negativeTemplate.findIndex((part) =>
    numberPartTypes.has(part.type),
  );
  let lastNumber = -1;
  for (let index = negativeTemplate.length - 1; index >= 0; index -= 1) {
    const part = negativeTemplate[index];
    if (part && numberPartTypes.has(part.type)) {
      lastNumber = index;
      break;
    }
  }
  if (firstNumber < 0 || lastNumber < 0) {
    throw new Error("The selected locale did not provide numeric money parts.");
  }
  const exactNumber = positiveParts.filter((part) =>
    numberPartTypes.has(part.type),
  );

  return [
    ...negativeTemplate.slice(0, firstNumber),
    ...exactNumber,
    ...negativeTemplate.slice(lastNumber + 1),
  ]
    .map((part) => part.value)
    .join("");
}

export function formatWeight(grams?: number | null) {
  if (grams == null) return "—";
  return grams >= 1000
    ? `${new Intl.NumberFormat("en-NG", { maximumFractionDigits: 2 }).format(grams / 1000)} kg`
    : `${grams} g`;
}

export function formatDateTime(
  value?: string | null,
  timeZone = "Africa/Lagos",
) {
  if (!value) return "—";
  return new Intl.DateTimeFormat("en-NG", {
    dateStyle: "medium",
    timeStyle: "short",
    timeZone,
  }).format(new Date(value));
}

export function titleCase(value: string) {
  return value
    .toLowerCase()
    .split("_")
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ");
}

export function initials(name?: string) {
  return (name ?? "User")
    .split(/\s+/)
    .slice(0, 2)
    .map((part) => part[0]?.toUpperCase())
    .join("");
}

export function safeDownloadName(value: string | undefined, fallback: string) {
  const withoutControls = Array.from(value ?? "")
    .map((character) => {
      const code = character.charCodeAt(0);
      return code >= 32 && code !== 127 ? character : "-";
    })
    .join("");
  const normalized = withoutControls
    .normalize("NFKC")
    .replace(/[<>:"/\\|?*]+/g, "-")
    .replace(/^\.+|\.+$/g, "")
    .replace(/\s+/g, " ")
    .trim();
  return (normalized || fallback).slice(0, 160);
}

export function toMinorUnits(value: string | number | undefined) {
  if (value === "" || value == null) return undefined;
  const number = typeof value === "number" ? value : Number(value);
  return Number.isFinite(number) ? Math.round(number * 100) : undefined;
}

export function pincodeIsValid(value: string) {
  return /^[1-9][0-9]{5}$/.test(value);
}
