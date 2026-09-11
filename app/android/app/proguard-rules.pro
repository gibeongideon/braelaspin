# Flutter's engine reaches these reflectively.
-keep class io.flutter.app.** { *; }
-keep class io.flutter.plugin.** { *; }
-keep class io.flutter.embedding.** { *; }

# Our platform-channel entry points, called from Dart by name.
-keep class com.dibon.braelaspin.MainActivity { *; }
-keep class com.dibon.braelaspin.SecureStore { *; }
