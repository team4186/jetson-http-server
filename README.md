# jetson-http-server

# Daemonizing
just copy `jetson-http-daemon` to `init.d` and use `sudo update-rc.d jetson-http-daemon defaults`. After that besides the server launch at boot, you can user `service jetson-http-daemon start|stop|restart|status`. `status` will have the last output lines which can be usefull for debugging.
