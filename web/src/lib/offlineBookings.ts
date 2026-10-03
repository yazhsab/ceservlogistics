import {
  apiRequest,
  type BookingPreview,
  type BookingRequest,
  type Shipment,
} from "../api/client";

const DATABASE_NAME = "ceserv-offline-bookings";
const DATABASE_VERSION = 1;
const DRAFT_STORE = "drafts";
const KEY_STORE = "keys";
const DEVICE_KEY_ID = "device-aes-gcm-key";

export type OfflineBookingSource = "BOOKING_FORM" | "MANUAL_IMPORT";
export type OfflineBookingStatus = "DRAFT" | "FAILED";

interface EncryptedPayload {
  request: BookingRequest;
  insuranceAccepted: boolean;
}

interface StoredDraft {
  id: string;
  createdAt: string;
  updatedAt: string;
  source: OfflineBookingSource;
  status: OfflineBookingStatus;
  idempotencyKey: string;
  manualWaybillNumber?: string;
  customerId: string;
  routeLabel: string;
  packageCount: number;
  lastError?: string;
  iv: ArrayBuffer;
  ciphertext: ArrayBuffer;
}

export interface OfflineBookingDraft
  extends Omit<StoredDraft, "iv" | "ciphertext"> {
  request: BookingRequest;
  insuranceAccepted: boolean;
}

export interface SaveOfflineBookingOptions {
  source: OfflineBookingSource;
  insuranceAccepted?: boolean;
  id?: string;
  idempotencyKey?: string;
}

export interface OfflineBookingResult {
  preview: BookingPreview;
  shipment: Shipment;
}

function assertOfflineStorageAvailable() {
  if (!window.indexedDB || !window.crypto?.subtle)
    throw new Error(
      "Encrypted offline drafts are not supported by this browser. Use a current Chrome, Edge, Safari, or Firefox release.",
    );
}

function requestResult<T>(request: IDBRequest<T>) {
  return new Promise<T>((resolve, reject) => {
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error ?? new Error("Offline storage failed."));
  });
}

function transactionComplete(transaction: IDBTransaction) {
  return new Promise<void>((resolve, reject) => {
    transaction.oncomplete = () => resolve();
    transaction.onerror = () =>
      reject(transaction.error ?? new Error("Offline storage failed."));
    transaction.onabort = () =>
      reject(transaction.error ?? new Error("Offline storage was interrupted."));
  });
}

async function openDatabase() {
  assertOfflineStorageAvailable();
  return new Promise<IDBDatabase>((resolve, reject) => {
    const request = window.indexedDB.open(DATABASE_NAME, DATABASE_VERSION);
    request.onupgradeneeded = () => {
      const database = request.result;
      if (!database.objectStoreNames.contains(DRAFT_STORE))
        database.createObjectStore(DRAFT_STORE, { keyPath: "id" });
      if (!database.objectStoreNames.contains(KEY_STORE))
        database.createObjectStore(KEY_STORE);
    };
    request.onsuccess = () => resolve(request.result);
    request.onerror = () =>
      reject(request.error ?? new Error("Could not open encrypted offline storage."));
  });
}

async function deviceEncryptionKey(database: IDBDatabase) {
  const read = database.transaction(KEY_STORE, "readonly");
  const existing = await requestResult<CryptoKey | undefined>(
    read.objectStore(KEY_STORE).get(DEVICE_KEY_ID) as IDBRequest<
      CryptoKey | undefined
    >,
  );
  await transactionComplete(read);
  if (existing) return existing;

  const created = await window.crypto.subtle.generateKey(
    { name: "AES-GCM", length: 256 },
    false,
    ["encrypt", "decrypt"],
  );
  const write = database.transaction(KEY_STORE, "readwrite");
  write.objectStore(KEY_STORE).put(created, DEVICE_KEY_ID);
  await transactionComplete(write);
  return created;
}

async function encryptPayload(
  database: IDBDatabase,
  payload: EncryptedPayload,
) {
  const key = await deviceEncryptionKey(database);
  const iv = window.crypto.getRandomValues(new Uint8Array(12));
  const plaintext = new TextEncoder().encode(JSON.stringify(payload));
  const ciphertext = await window.crypto.subtle.encrypt(
    { name: "AES-GCM", iv },
    key,
    plaintext,
  );
  return { iv: iv.buffer, ciphertext };
}

async function decryptPayload(
  database: IDBDatabase,
  stored: StoredDraft,
) {
  const key = await deviceEncryptionKey(database);
  const plaintext = await window.crypto.subtle.decrypt(
    { name: "AES-GCM", iv: new Uint8Array(stored.iv) },
    key,
    stored.ciphertext,
  );
  return JSON.parse(new TextDecoder().decode(plaintext)) as EncryptedPayload;
}

function routeLabel(request: BookingRequest) {
  const origin = request.sender.city || request.sender.pincode || "Origin";
  const destination =
    request.recipient.city || request.recipient.pincode || "Destination";
  return `${origin} → ${destination}`;
}

function publicDraft(stored: StoredDraft, payload: EncryptedPayload) {
  return {
    id: stored.id,
    createdAt: stored.createdAt,
    updatedAt: stored.updatedAt,
    source: stored.source,
    status: stored.status,
    idempotencyKey: stored.idempotencyKey,
    manualWaybillNumber: stored.manualWaybillNumber,
    customerId: stored.customerId,
    routeLabel: stored.routeLabel,
    packageCount: stored.packageCount,
    lastError: stored.lastError,
    ...payload,
  } satisfies OfflineBookingDraft;
}

export async function saveOfflineBookingDraft(
  request: BookingRequest,
  options: SaveOfflineBookingOptions,
) {
  const database = await openDatabase();
  try {
    const id = options.id ?? crypto.randomUUID();
    const now = new Date().toISOString();
    let createdAt = now;
    if (options.id) {
      const transaction = database.transaction(DRAFT_STORE, "readonly");
      const previous = await requestResult<StoredDraft | undefined>(
        transaction.objectStore(DRAFT_STORE).get(options.id) as IDBRequest<
          StoredDraft | undefined
        >,
      );
      await transactionComplete(transaction);
      createdAt = previous?.createdAt ?? now;
    }
    const encrypted = await encryptPayload(database, {
      request,
      insuranceAccepted: options.insuranceAccepted ?? false,
    });
    const stored: StoredDraft = {
      id,
      createdAt,
      updatedAt: now,
      source: options.source,
      status: "DRAFT",
      idempotencyKey: options.idempotencyKey ?? crypto.randomUUID(),
      manualWaybillNumber: request.manualWaybillNumber,
      customerId: request.customerId,
      routeLabel: routeLabel(request),
      packageCount: request.packages.length,
      ...encrypted,
    };
    const transaction = database.transaction(DRAFT_STORE, "readwrite");
    transaction.objectStore(DRAFT_STORE).put(stored);
    await transactionComplete(transaction);
    return publicDraft(stored, {
      request,
      insuranceAccepted: options.insuranceAccepted ?? false,
    });
  } finally {
    database.close();
  }
}

export async function listOfflineBookingDrafts() {
  const database = await openDatabase();
  try {
    const transaction = database.transaction(DRAFT_STORE, "readonly");
    const stored = await requestResult<StoredDraft[]>(
      transaction.objectStore(DRAFT_STORE).getAll() as IDBRequest<StoredDraft[]>,
    );
    await transactionComplete(transaction);
    const drafts = await Promise.all(
      stored.map(async (item) => publicDraft(item, await decryptPayload(database, item))),
    );
    return drafts.sort((left, right) => right.updatedAt.localeCompare(left.updatedAt));
  } finally {
    database.close();
  }
}

export async function deleteOfflineBookingDraft(id: string) {
  const database = await openDatabase();
  try {
    const transaction = database.transaction(DRAFT_STORE, "readwrite");
    transaction.objectStore(DRAFT_STORE).delete(id);
    await transactionComplete(transaction);
  } finally {
    database.close();
  }
}

async function getStoredDraft(database: IDBDatabase, id: string) {
  const transaction = database.transaction(DRAFT_STORE, "readonly");
  const stored = await requestResult<StoredDraft | undefined>(
    transaction.objectStore(DRAFT_STORE).get(id) as IDBRequest<
      StoredDraft | undefined
    >,
  );
  await transactionComplete(transaction);
  if (!stored) throw new Error("This offline booking draft no longer exists.");
  return stored;
}

async function recordFailure(id: string, error: unknown) {
  const database = await openDatabase();
  try {
    const stored = await getStoredDraft(database, id);
    stored.status = "FAILED";
    stored.updatedAt = new Date().toISOString();
    stored.lastError = error instanceof Error ? error.message : "Booking failed.";
    const transaction = database.transaction(DRAFT_STORE, "readwrite");
    transaction.objectStore(DRAFT_STORE).put(stored);
    await transactionComplete(transaction);
  } finally {
    database.close();
  }
}

export async function bookOfflineBookingDraft(
  id: string,
): Promise<OfflineBookingResult> {
  if (!navigator.onLine)
    throw new Error("Reconnect to the internet before validating this booking.");
  const database = await openDatabase();
  let stored: StoredDraft;
  let payload: EncryptedPayload;
  try {
    stored = await getStoredDraft(database, id);
    payload = await decryptPayload(database, stored);
  } finally {
    database.close();
  }

  try {
    const previewRequest: BookingRequest = {
      ...payload.request,
      insuranceAcceptance: undefined,
    };
    const preview = await apiRequest<BookingPreview>(
      "/api/v1/shipments/preview",
      { method: "POST", body: previewRequest },
    );
    if (!preview.serviceability.serviceable)
      throw new Error(
        preview.serviceability.reasonMessage ??
          "This shipment is not serviceable with the current network configuration.",
      );

    const insuranceAcceptance = payload.request.insuranceRequired
      ? payload.insuranceAccepted
        ? {
            accepted: true,
            quoteFingerprint: preview.quote.insurance?.quoteFingerprint,
          }
        : undefined
      : undefined;
    if (payload.request.insuranceRequired && !insuranceAcceptance)
      throw new Error(
        "Insurance was requested but customer acceptance was not recorded. Review this draft in New booking before booking it.",
      );
    if (
      payload.request.insuranceRequired &&
      !insuranceAcceptance?.quoteFingerprint
    )
      throw new Error("The server did not return an insurance quote for this shipment.");

    const shipment = await apiRequest<Shipment>("/api/v1/shipments", {
      method: "POST",
      headers: { "Idempotency-Key": stored.idempotencyKey },
      body: { ...previewRequest, insuranceAcceptance },
    });
    await deleteOfflineBookingDraft(id);
    return { preview, shipment };
  } catch (error) {
    await recordFailure(id, error);
    throw error;
  }
}
