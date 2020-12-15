#!/bin/bash
dove_dir=/usr/local/DoveClient/
if [ ! -d $dove_dir ]; then
   mkdir -p $dove_dir
   if [ $? -ne 0 ]; then
      exit 1
   fi
fi
os_type=`uname -s`
echo "copying DoveClient to $dove_dir"  && cp DoveClient.$os_type $dove_dir/DoveClient
if [ $? -ne 0 ]; then
   exit 1
fi

if [ -f /etc/DoveClient.conf ]; then
   echo "Old /etc/DoveClient.conf found, skip this file.."
else
    echo "copying  DoveClient.conf to /etc/..." && cp etc/DoveClient.conf /etc/
    if [ $? -ne 0 ]; then
       exit 1
    fi
fi


echo "installing system service..."
case "$os_type" in
    Darwin)
        cp DoveClient.darwin.sh /usr/bin/DoveClient
        chmod 755 /usr/bin/DoveClient
        cp com.jumei.doveclient.plist /Library/LaunchDaemons/
        echo "Installation complete. You have to fix configs in /etc/DoveClient.conf before you can start the DoveClient."
        echo "Use command 'DoveClient start' to start the client"
   ;;
    Linux)
        cp etc/init.d/DoveClient /etc/init.d/
        issue=$(head  -n 1 /etc/issue | sed 's/^\([^ ]*\) [0-9]*.*/\1/')
        case "$issue" in
            Ubuntu|Debian)
                update-rc.d DoveClient defaults
	    ;;
	    CentOS|RedHat)
	        chkconfig DoveClient on
	    ;;
        esac
        echo "Installation complete. You have to fix configs in /etc/DoveClient.conf before you can start the DoveClient by executing 'sudo service DoveClient start'" 
   ;;
esac
