# Flutter's engine reaches these reflectively.
-keep class io.flutter.app.** { *; }
-keep class io.flutter.plugin.** { *; }
-keep class io.flutter.embedding.** { *; }

# Our platform-channel entry points, called from Dart by name.
-keep class com.dibon.braelaspin.MainActivity { *; }
-keep class com.dibon.braelaspin.SecureStore { *; }

# Play Core / deferred components.
#
# Flutter's engine references SplitInstallManager so that apps using deferred
# components can load them at runtime. We do not use deferred components and do
# not ship the Play Core library, so R8 sees the references dangling and fails
# the build. Silencing them is correct rather than papering over a problem: the
# code path is unreachable in this app, and adding Play Core purely to satisfy
# a reference we never call would add weight to an APK with a 10MB budget.
-dontwarn com.google.android.play.core.**
-dontwarn io.flutter.embedding.engine.deferredcomponents.**
