package app.lorca.core

import android.content.Context
import android.os.Build
import android.provider.Settings
import java.io.File
import java.util.concurrent.CopyOnWriteArraySet
import uniffi.lorca_mobile.Core
import uniffi.lorca_mobile.EventListener

/// The one Rust core of the process, shared by the JS module and the background service
/// (SyncService): whichever starts first starts it, and every event reaches both.
object CoreHolder {
  @Volatile var core: Core? = null
    private set

  private val listeners = CopyOnWriteArraySet<(String) -> Unit>()

  fun addListener(listener: (String) -> Unit) = listeners.add(listener)

  fun removeListener(listener: (String) -> Unit) = listeners.remove(listener)

  /// Starts the core under `home` with the phone's facts, once; later calls get the running one.
  @Synchronized fun start(home: String, name: String, os: String, osVersion: String, model: String): Core {
    core?.let { return it }
    val started = Core.start(home, name, os, osVersion, model, object : EventListener {
      override fun onEvent(json: String) {
        for (listener in listeners) listener(json)
      }
    })
    core = started
    return started
  }

  /// Starts the core without the JS side, as the service does when the system restarts it: the
  /// same folder and the facts `hostFacts()` in src/core/host.ts reads.
  fun startHere(context: Context): Core {
    val model = Build.MODEL ?: "Android"
    val name = Settings.Global.getString(context.contentResolver, "device_name")?.trim()?.takeIf { it.isNotEmpty() } ?: model
    return start(home(context), name, "android", "Android ${Build.VERSION.RELEASE}", model)
  }

  /// The folder the JS side starts the core with (`coreHome()` in src/core/prefs.ts).
  fun home(context: Context): String {
    val development = context.packageName == "app.lorca.dev"
    return File(context.filesDir, "${if (development) "lorca-dev" else "lorca"}/core").absolutePath
  }
}
