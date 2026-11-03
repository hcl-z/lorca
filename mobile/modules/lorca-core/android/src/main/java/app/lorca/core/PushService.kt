package app.lorca.core

import android.content.Context
import com.google.firebase.messaging.FirebaseMessagingService
import com.google.firebase.messaging.RemoteMessage
import uniffi.lorca_mobile.pushOpen

/// A push is a data message carrying ciphertext (`c`). The core opens it with the account key
/// under the app's folder, no running core needed, and the notification shows who replied,
/// where, and the first words. A tap opens the chat through this build's app link.
class PushService : FirebaseMessagingService() {
  override fun onMessageReceived(message: RemoteMessage) {
    // The background service's sync socket is open: it alerts for the same reply by itself.
    if (SyncService.running && CoreHolder.core?.relayConnected() == true) return
    val sealed = message.data["c"] ?: return
    val notice = try { pushOpen(CoreHolder.home(this), sealed) } catch (_: Throwable) { null }
    // The user is looking at that chat: the reply is arriving in front of them.
    if (notice != null && inFront && notice.chatId == openChat) return
    val body = notice?.body ?: "New reply"
    Notices.post(this, notice?.title, notice?.subtitle, notice?.body, notice?.chatId, (message.messageId ?: body).hashCode())
  }

  // The app registers the current token with the relay each time it starts (src/core/push.ts).
  override fun onNewToken(token: String) {}

  companion object {
    /// The chat on screen, or null: the JS side keeps it current through the module's
    /// `setOpenChat`.
    @Volatile var openChat: String? = null

    /// Whether the app is in front. The module follows the host's resume and pause here
    /// rather than through JS: a push can land a moment after the user leaves, while the JS
    /// thread is still busy with the reply that came with it.
    @Volatile var inFront = false

    fun clear(context: Context, chatId: String) = Notices.clear(context, chatId)
  }
}
