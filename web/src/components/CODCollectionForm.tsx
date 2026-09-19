import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import {
  ApiError,
  apiRequest,
  operationalHeaders,
  type CODCollectionRequest,
  type CODCollectionResult,
} from "../api/client";
import { formatMoney } from "../lib/utils";
import { Button, ErrorState, Field, InlineNotice, Input, Select } from "./ui";

/** Records custody explicitly; delivery completion is a separate operation. */
export function CODCollectionForm({
  shipmentId,
  amountMinor,
  currency,
}: {
  shipmentId: string;
  amountMinor: number;
  currency?: string;
}) {
  const client = useQueryClient();
  const [mode, setMode] = useState<CODCollectionRequest["paymentMode"] | "">(
    "",
  );
  const [reference, setReference] = useState("");
  const [request, setRequest] = useState<CODCollectionRequest>();
  const [headers] = useState(() => operationalHeaders("cod-collection"));
  const mutation = useMutation({
    mutationFn: (body: CODCollectionRequest) =>
      apiRequest<CODCollectionResult>("/api/v1/cod/collections", {
        method: "POST",
        body,
        headers,
      }),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: ["cod-summary"] });
      void client.invalidateQueries({ queryKey: ["cod-obligations"] });
      void client.invalidateQueries({ queryKey: ["cod-obligation"] });
    },
  });
  if (mutation.data)
    return (
      <InlineNotice tone="success" title="COD custody recorded">
        {formatMoney(
          mutation.data.collection?.amountMinor,
          mutation.data.collection?.currency,
        )}{" "}
        is recorded against this shipment. Follow your branch's cash handover
        procedure.
      </InlineNotice>
    );
  if (
    mutation.error instanceof ApiError &&
    mutation.error.code === "COD_ALREADY_COLLECTED"
  )
    return (
      <InlineNotice tone="info" title="COD already recorded">
        This shipment already has a collection record. Finance can review its
        custody history.
      </InlineNotice>
    );
  return (
    <section
      aria-label="Record COD custody"
      className="space-y-3 rounded-md border border-border p-4 text-left"
    >
      <h2 className="font-semibold">Record COD custody</h2>
      <p className="text-sm text-slate-600">
        Delivery recorded {formatMoney(amountMinor, currency)} collected.
        Confirm the payment method to record the money in your custody.
      </p>
      <Field
        label="Collection payment method"
        htmlFor="cod-custody-mode"
        required
      >
        <Select
          id="cod-custody-mode"
          value={mode}
          disabled={Boolean(request)}
          onChange={(event) =>
            setMode(event.target.value as CODCollectionRequest["paymentMode"])
          }
        >
          <option value="">Choose payment method</option>
          {(
            [
              "CASH",
              "UPI",
              "CARD",
              "WALLET",
              "NET_BANKING",
              "CHEQUE",
              "DEMAND_DRAFT",
              "OTHER",
            ] as const
          ).map((value) => (
            <option key={value}>{value}</option>
          ))}
        </Select>
      </Field>
      <Field
        label="Collection reference"
        htmlFor="cod-custody-ref"
        required={mode !== "" && mode !== "CASH"}
      >
        <Input
          id="cod-custody-ref"
          value={reference}
          disabled={Boolean(request)}
          onChange={(event) => setReference(event.target.value)}
        />
      </Field>
      {mutation.error ? <ErrorState error={mutation.error} /> : null}
      <Button
        variant="primary"
        loading={mutation.isPending}
        disabled={!mode || (mode !== "CASH" && !reference.trim())}
        onClick={() => {
          if (!mode) return;
          const body = request ?? {
            shipmentId,
            amountMinor,
            paymentMode: mode,
            reference: reference.trim() || undefined,
          };
          setRequest(body);
          mutation.mutate(body);
        }}
      >
        {request ? "Retry COD recording" : "Record COD collection"}
      </Button>
      {request && mutation.error ? (
        <p className="text-xs text-slate-500">
          Retry uses the same details and request identifier to prevent
          duplicate recording.
        </p>
      ) : null}
    </section>
  );
}
