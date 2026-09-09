"""Disposable loopback servers with real OPC UA and SNMP authentication."""
import asyncio
import datetime
import hashlib
import ipaddress
import json
import logging
import os
from pathlib import Path
import socket
import sys


def free_port(socktype):
    with socket.socket(socket.AF_INET, socktype) as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


async def main(root):
    from argparse import Namespace
    from bacpypes3.app import Application
    from bacpypes3.local.analog import AnalogInputObject
    from asyncua import Server, ua
    from asyncua.crypto.permission_rules import User, UserRole
    from cryptography import x509
    from cryptography.hazmat.primitives import hashes, serialization
    from cryptography.hazmat.primitives.asymmetric import rsa
    from cryptography.x509.oid import NameOID
    from pysnmp.entity import engine, config
    from pysnmp.carrier.asyncio.dgram import udp
    from pysnmp.entity.rfc3413 import context, cmdrsp

    root.mkdir(parents=True, exist_ok=True)
    def certificate(name):
        key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
        subject = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, name)])
        now = datetime.datetime.now(datetime.timezone.utc)
        cert = (x509.CertificateBuilder().subject_name(subject).issuer_name(subject)
                .public_key(key.public_key()).serial_number(x509.random_serial_number())
                .not_valid_before(now-datetime.timedelta(minutes=1))
                .not_valid_after(now+datetime.timedelta(days=1))
                .add_extension(x509.SubjectAlternativeName([
                    x509.DNSName("localhost"), x509.IPAddress(ipaddress.ip_address("127.0.0.1")),
                    x509.UniformResourceIdentifier("urn:iot:"+name)]), critical=False)
                .sign(key, hashes.SHA256()))
        cert_path, key_path = root/(name+".der"), root/(name+".pem")
        cert_path.write_bytes(cert.public_bytes(serialization.Encoding.DER))
        key_path.write_bytes(key.private_bytes(serialization.Encoding.PEM,
                              serialization.PrivateFormat.PKCS8, serialization.NoEncryption()))
        os.chmod(key_path, 0o600)
        return cert_path, key_path

    server_cert, server_key = certificate("fixture")
    client_cert, client_key = certificate("client")
    class Users:
        def get_user(self, iserver, username=None, password=None, certificate=None):
            if username == "fixture-user" and password == "fixture-auth-password":
                return User(role=UserRole.User)
            return None

    opc_port, snmp_port = free_port(socket.SOCK_STREAM), free_port(socket.SOCK_DGRAM)
    opc = Server(user_manager=Users())
    await opc.init()
    opc.set_endpoint(f"opc.tcp://127.0.0.1:{opc_port}")
    await opc.set_application_uri("urn:iot:fixture")
    opc.set_security_policy([ua.SecurityPolicyType.Basic256Sha256_SignAndEncrypt])
    opc.set_identity_tokens([ua.UserNameIdentityToken])
    await opc.load_certificate(server_cert)
    await opc.load_private_key(server_key)
    namespace = await opc.register_namespace("urn:iot:read-test")
    await opc.nodes.objects.add_variable(ua.NodeId("temperature", namespace), "Temperature", 42.0)

    snmp = engine.SnmpEngine()
    config.add_transport(snmp, udp.DOMAIN_NAME, udp.UdpTransport().open_server_mode(("127.0.0.1", snmp_port)))
    config.add_v3_user(snmp, "fixture-user", config.USM_AUTH_HMAC192_SHA256,
                       "fixture-auth-password", config.USM_PRIV_CFB128_AES, "fixture-privacy-password")
    config.add_vacm_user(snmp, 3, "fixture-user", "authPriv", (1,3,6,1,2,1))
    config.add_v1_system(snmp, "fixture-v2", "fixture-community")
    config.add_vacm_user(snmp, 2, "fixture-v2", "noAuthNoPriv", (1,3,6,1,2,1))
    mib = snmp.get_mib_builder()
    (description,) = mib.import_symbols("SNMPv2-MIB", "sysDescr")
    (instance,) = mib.import_symbols("SNMPv2-SMI", "MibScalarInstance")
    mib.export_symbols("IOT-TEST", instance(description.name, (0,), description.syntax.clone("actual-snmp-response")))
    cmdrsp.GetCommandResponder(snmp, context.SnmpContext(snmp))
    bacnet_port = free_port(socket.SOCK_DGRAM)
    bacnet = Application.from_args(Namespace(vendoridentifier=999, instance=1234, name="fixture", address=f"127.0.0.1:{bacnet_port}", network=0, foreign=None, bbmd=None, ttl=30))
    bacnet.add_object(AnalogInputObject(objectIdentifier=("analogInput", 1), objectName="Temperature", presentValue=42.0, units="degreesCelsius"))
    async with opc:
        print(json.dumps({"opcPort":opc_port,"snmpPort":snmp_port,"bacnetPort":bacnet_port,"nodeId":f"ns={namespace};s=temperature",
                          "certificateFile":str(client_cert),"privateKeyFile":str(client_key),
                          "serverSha256":hashlib.sha256(server_cert.read_bytes()).hexdigest()}), flush=True)
        await asyncio.Event().wait()


if __name__ == "__main__":
    logging.basicConfig(level=logging.ERROR)
    asyncio.run(main(Path(sys.argv[1])))
