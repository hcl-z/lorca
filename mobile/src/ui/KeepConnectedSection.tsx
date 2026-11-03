// Android's Keep Connected in Settings: a foreground service keeps the sync socket open in the
// background, so replies and questions alert from the phone at once, without a push service.
// Under it, what lets the service keep running: battery optimization, and the switches some
// phone makers add for running in the background and starting by itself.

import { useEffect, useState } from "react";
import { AppState } from "react-native";
import * as core from "../../modules/lorca-core";
import { t } from "../i18n";
import { Row, Section, ToggleRow } from "./forms";

export function KeepConnectedSection() {
  const [status, setStatus] = useState(() => core.backgroundStatus());
  // Back from the system's prompt or the maker's page: read what changed there.
  useEffect(() => {
    const subscription = AppState.addEventListener("change", (state) => {
      if (state === "active") setStatus(core.backgroundStatus());
    });
    return () => subscription.remove();
  }, []);
  if (!core.canKeepConnected || !status) return null;
  return (
    <Section
      title={t("Background")}
      footer={t("Keeps the connection to your Devices open while Lorca is in the background, so replies and questions alert at once. Android shows a notification while it runs, and it uses more battery.")}
    >
      <ToggleRow
        title={t("Keep Connected")}
        value={status.enabled}
        onValueChange={(on) => {
          core.setKeepConnected(on);
          setStatus(core.backgroundStatus());
        }}
      />
      {status.enabled && (
        <Row
          title={t("Battery Optimization")}
          subtitle={status.batteryExempt ? t("Lorca may run in the background") : t("Android may stop Lorca in the background")}
          onPress={status.batteryExempt ? undefined : () => core.requestBatteryExemption()}
          chevron={!status.batteryExempt}
        />
      )}
      {status.enabled && status.makerSettings && (
        <Row
          title={t("Background Running and Auto-start")}
          subtitle={t("This phone has its own switches; allow both for Lorca")}
          subtitleLines={2}
          onPress={() => core.openMakerSettings()}
          chevron
        />
      )}
    </Section>
  );
}
